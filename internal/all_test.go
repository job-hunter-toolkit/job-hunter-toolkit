package internal_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// staticSource returns a job source that yields one posting per title.
func staticSource(company string, titles ...string) internal.JobsFunc {
	return func(ctx context.Context, _ *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			for _, title := range titles {
				if !yield(&internal.JobPosting{
					Company:  company,
					Title:    title,
					URL:      "https://example.test/" + company + "/" + title,
					Location: "Remote",
				}, nil) {
					return
				}
			}
		}
	}
}

// failingSource returns a job source that only yields err.
func failingSource(err error) internal.JobsFunc {
	return func(ctx context.Context, _ *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			yield(nil, err)
		}
	}
}

// collect drains a Jobs sequence into postings and errors.
func collect(jobs internal.Jobs) (titles []string, errs []error) {
	for job, err := range jobs {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		titles = append(titles, job.Title)
	}

	return titles, errs
}

func TestAllYieldsEveryPosting(t *testing.T) {
	t.Parallel()

	titles, errs := collect(internal.All(t.Context(), nil,
		staticSource("acme", "a1", "a2"),
		staticSource("globex", "g1"),
		staticSource("initech", "i1", "i2", "i3"),
	))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	slices.Sort(titles)

	want := []string{"a1", "a2", "g1", "i1", "i2", "i3"}
	if !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v", titles, want)
	}
}

func TestAllContinuesPastFailingSource(t *testing.T) {
	t.Parallel()

	// The whole point: one dead company must not end a crawl of a thousand.
	wantErr := errors.New("board is gone")

	titles, errs := collect(internal.All(t.Context(), nil,
		failingSource(wantErr),
		staticSource("acme", "a1"),
		failingSource(wantErr),
		staticSource("globex", "g1"),
	))

	if len(errs) != 2 {
		t.Errorf("got %d errors, want 2", len(errs))
	}

	for _, err := range errs {
		if !errors.Is(err, wantErr) {
			t.Errorf("error = %v, want it to wrap %v", err, wantErr)
		}
	}

	slices.Sort(titles)

	if want := []string{"a1", "g1"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v", titles, want)
	}
}

func TestAllRecoversPanickingSource(t *testing.T) {
	t.Parallel()

	panicking := func(ctx context.Context, _ *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			panic("adapter parsed something unexpected")
		}
	}

	titles, errs := collect(internal.All(t.Context(), nil,
		panicking,
		staticSource("acme", "a1"),
	))

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}

	if !strings.Contains(errs[0].Error(), "panicked") {
		t.Errorf("error = %v, want it to mention the panic", errs[0])
	}

	// The healthy source must still have been crawled.
	if want := []string{"a1"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v", titles, want)
	}
}

func TestAllStopsWhenConsumerBreaks(t *testing.T) {
	t.Parallel()

	var yielded atomic.Int64

	// A source with far more postings than will be consumed.
	big := func(ctx context.Context, _ *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			for i := range 10_000 {
				if !yield(&internal.JobPosting{Company: "acme", Title: fmt.Sprint(i)}, nil) {
					return
				}
				yielded.Add(1)
			}
		}
	}

	got := 0
	for range internal.All(t.Context(), nil, big) {
		got++
		if got == 5 {
			break
		}
	}

	if got != 5 {
		t.Errorf("consumed %d postings, want 5", got)
	}

	// Producers are asked to stop, so the source must not have run to completion.
	if n := yielded.Load(); n > 1_000 {
		t.Errorf("source yielded %d postings after the consumer stopped; it should wind down", n)
	}
}

func TestAllDoesNotLeakGoroutines(t *testing.T) {
	// Not parallel: goroutine counting is process-wide.
	before := runtime.NumGoroutine()

	blocking := func(ctx context.Context, _ *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			for i := 0; ; i++ {
				if !yield(&internal.JobPosting{Company: "acme", Title: fmt.Sprint(i)}, nil) {
					return
				}
			}
		}
	}

	sources := make([]internal.JobsFunc, 0, 50)
	for range 50 {
		sources = append(sources, blocking)
	}

	// Abandon the iteration early, leaving many sources mid-send.
	n := 0
	for range internal.All(t.Context(), nil, sources...) {
		n++
		if n == 10 {
			break
		}
	}

	// Goroutine teardown is asynchronous, so allow it a moment to settle.
	var after int
	for range 100 {
		after = runtime.NumGoroutine()
		if after <= before+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Errorf("goroutines before = %d, after = %d; sources were left running", before, after)
}

func TestAllRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	// Cancel before consuming anything.
	cancel()

	titles, errs := collect(internal.AllWithConcurrency(ctx, nil, 4,
		staticSource("acme", "a1", "a2"),
		staticSource("globex", "g1"),
	))

	if len(titles) != 0 {
		t.Errorf("titles = %v, want none once the context is cancelled", titles)
	}

	// The caller must be able to tell a truncated crawl from an empty one.
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Errorf("errs = %v, want a single context.Canceled", errs)
	}
}

func TestAllWithConcurrencyOneIsDeterministic(t *testing.T) {
	t.Parallel()

	// A limit of 1 makes the crawl sequential, so source order is preserved.
	// Tests and reproducible reports depend on this.
	titles, errs := collect(internal.AllWithConcurrency(t.Context(), nil, 1,
		staticSource("acme", "a1", "a2"),
		staticSource("globex", "g1"),
		staticSource("initech", "i1"),
	))

	if len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	want := []string{"a1", "a2", "g1", "i1"}
	if !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v in source order", titles, want)
	}
}

func TestAllRunsSourcesConcurrently(t *testing.T) {
	t.Parallel()

	const sources = 8

	var (
		active atomic.Int64
		peak   atomic.Int64
	)

	slow := func(ctx context.Context, _ *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			n := active.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}

			// Long enough that sequential execution could not overlap.
			time.Sleep(50 * time.Millisecond)
			active.Add(-1)

			yield(&internal.JobPosting{Company: "acme", Title: "t"}, nil)
		}
	}

	list := make([]internal.JobsFunc, 0, sources)
	for range sources {
		list = append(list, slow)
	}

	start := time.Now()

	titles, _ := collect(internal.AllWithConcurrency(t.Context(), nil, sources, list...))

	elapsed := time.Since(start)

	if len(titles) != sources {
		t.Errorf("got %d postings, want %d", len(titles), sources)
	}

	if peak.Load() < 2 {
		t.Errorf("peak concurrent sources = %d, want at least 2", peak.Load())
	}

	// Sequential execution would take sources * 50ms.
	if elapsed > time.Duration(sources)*50*time.Millisecond {
		t.Errorf("elapsed = %v, want well under sequential time", elapsed)
	}
}

func TestAllRespectsConcurrencyLimit(t *testing.T) {
	t.Parallel()

	const limit = 3

	var (
		active atomic.Int64
		peak   atomic.Int64
	)

	counting := func(ctx context.Context, _ *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			n := active.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}

			time.Sleep(20 * time.Millisecond)
			active.Add(-1)

			yield(&internal.JobPosting{Company: "acme", Title: "t"}, nil)
		}
	}

	list := make([]internal.JobsFunc, 0, 20)
	for range 20 {
		list = append(list, counting)
	}

	if _, errs := collect(internal.AllWithConcurrency(t.Context(), nil, limit, list...)); len(errs) != 0 {
		t.Fatalf("errors = %v, want none", errs)
	}

	if got := peak.Load(); got > limit {
		t.Errorf("peak concurrent sources = %d, want at most %d", got, limit)
	}
}

func TestAllHandlesNoSources(t *testing.T) {
	t.Parallel()

	titles, errs := collect(internal.All(t.Context(), nil))

	if len(titles) != 0 || len(errs) != 0 {
		t.Errorf("got titles = %v, errs = %v, want both empty", titles, errs)
	}
}
