package internal

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"sync"
)

// Jobs is a sequence of job postings, or an error if one occurs
// while fetching the postings. The first error might not be the end
// of the sequence, depending on how the jobs are sourced.
type Jobs = iter.Seq2[*JobPosting, error]

// JobsFunc is a function that returns a sequence of job postings
// from a job source. It returns a sequence of jobs, or an error
// if one occurs while fetching the postings.
type JobsFunc func(context.Context, *http.Client) Jobs

// DefaultConcurrency is the number of job sources [All] fetches at once.
//
// This is a scheduling width, not a politeness setting, and the distinction is
// the whole reason the number can be this large. Politeness is enforced
// per-service by httpx's limiter, which caps concurrent requests to a single
// backend at httpx.DefaultPerHostLimit, which is 4 (2 for Workable and
// PeopleForce), and
// paces them. Adding workers cannot exceed that cap on any backend; it only
// stops the pool from idling while workers are parked on a service semaphore
// waiting for their turn. Nine hundred Greenhouse sources still go through four
// slots on boards-api.greenhouse.io whether the pool is 16 wide or 64.
//
// This used to be min(32, max(8, runtime.NumCPU()*4)). The comment claimed it
// was "well above the CPU count", but the value was still derived from it, and
// on a 4-vCPU GitHub runner that formula evaluates to 16, half the intended
// ceiling, for work that is 100% network-bound and burns essentially no CPU.
// At ~1,772 companies and a 60-minute budget, 16 workers give each source a
// mean of 29 seconds, and the nightly crawl has missed its deadline every day
// for weeks.
//
// 64 is sized against file descriptors, which is the resource that actually
// binds here. Sources that issue one request at a time hold one socket each,
// but that is no longer true of all of them: the Workday adapter fetches a
// tenant's remaining pages with a fan-out of workdayPageFetchers (4), and
// Workday tenant hosts are deliberately given their own limiter key, so those
// requests do not share a per-service cap with one another. A pool full of
// Workday sources therefore holds up to 64*4 = 256 sockets in flight, not 64.
// With httpx's 200-connection idle cache (MaxIdleConns) that is roughly 456
// descriptors against the default 1024 soft limit on a GitHub runner: still
// real headroom, but four times what a one-request-per-source model predicts.
//
// Any future adapter that fans out within a source multiplies this the same
// way. Raise the fan-out or the pool, but not both without redoing this sum;
// the crawl's success should not come to depend on ulimit.
var DefaultConcurrency = 64

// All finds all of the JobPostings using each of the provided job sources,
// fetching up to [DefaultConcurrency] sources concurrently.
//
// Postings arrive in whatever order they are fetched, not in job-source order.
// A source that fails yields its error and does not stop the others, so one
// dead company cannot end a crawl.
func All(ctx context.Context, httpClient *http.Client, jobSources ...JobsFunc) Jobs {
	return AllWithConcurrency(ctx, httpClient, DefaultConcurrency, jobSources...)
}

// AllWithConcurrency is [All] with an explicit limit on how many job sources
// are fetched at once. A limit below 1 is treated as 1, making the crawl
// sequential and its output deterministic, which is useful in tests.
func AllWithConcurrency(ctx context.Context, httpClient *http.Client, limit int, jobSources ...JobsFunc) Jobs {
	return func(yield func(*JobPosting, error) bool) {
		if limit < 1 {
			limit = 1
		}

		// Cancelling this context is how a consumer that stops early (via break,
		// or an error) tells in-flight sources to wind down.
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		type result struct {
			job *JobPosting
			err error
		}

		var (
			results = make(chan result)
			sem     = make(chan struct{}, limit)
			wg      sync.WaitGroup
		)

		go func() {
			// results must not be closed until every sender has finished, or a
			// straggling send would panic.
			defer func() {
				wg.Wait()
				close(results)
			}()

			for _, source := range jobSources {
				// Checked before the select below because a select with two ready
				// cases picks randomly, which would let work start after
				// cancellation.
				if ctx.Err() != nil {
					return
				}

				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}

				wg.Add(1)

				go func(source JobsFunc) {
					defer wg.Done()
					defer func() { <-sem }()

					send := func(r result) bool {
						// As above: checked first so cancellation wins over a
						// ready channel send.
						if ctx.Err() != nil {
							return false
						}

						select {
						case results <- r:
							return true
						case <-ctx.Done():
							return false
						}
					}

					// A crawl runs a lot of third-party HTML and JSON parsing. A
					// panic in one adapter is a bug worth surfacing, but it must
					// not take down a crawl covering a thousand other companies,
					// so it is reported as that source's error instead.
					defer func() {
						if r := recover(); r != nil {
							send(result{err: fmt.Errorf("job source panicked: %v", r)})
						}
					}()

					for jobPosting, err := range source(ctx, httpClient) {
						if !send(result{job: jobPosting, err: err}) {
							return
						}
					}
				}(source)
			}
		}()

		for r := range results {
			if !yield(r.job, r.err) {
				// Stop the producers, then drain until they have all exited so
				// no goroutine is left blocked on a send.
				cancel()
				for range results {
				}

				return
			}
		}

		// A crawl cut short by a deadline or cancellation has returned partial
		// results. Report that, so callers do not mistake a truncated crawl for
		// a complete one; the difference matters when the output is a
		// posting-count trend line.
		if err := ctx.Err(); err != nil {
			yield(nil, err)
		}
	}
}
