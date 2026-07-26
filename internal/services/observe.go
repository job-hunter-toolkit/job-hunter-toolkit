package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

const (
	sourcePlanned   = "planned"
	sourceRunning   = "running"
	sourceComplete  = "complete"
	sourceFailed    = "failed"
	sourceTruncated = "truncated"
	sourceStopped   = "stopped"
)

// SourceRun describes one source's contribution to a crawl.
//
// Posting counts are before global URL deduplication. ErrorClass is deliberately
// coarse so manifests remain useful without storing response bodies, raw URLs,
// credentials, or other sensitive and high-cardinality error details.
type SourceRun struct {
	Platform   string    `json:"platform"`
	Key        string    `json:"key"`
	Company    string    `json:"company"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	DurationMS int64     `json:"duration_ms,omitzero"`
	Postings   int       `json:"postings"`
	Errors     int       `json:"errors"`
	ErrorClass string    `json:"error_class,omitempty"`
}

// Observe wraps sources with lifecycle measurement and returns a snapshot
// function for their results. Call results only after the wrapped jobs have
// stopped to obtain a final manifest; it is safe to call while a crawl runs for
// progress reporting.
func Observe(sources []Source, logger *slog.Logger) ([]internal.JobsFunc, func() []SourceRun) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	runs := make([]SourceRun, len(sources))
	for i, source := range sources {
		runs[i] = SourceRun{
			Platform: source.Platform,
			Key:      source.Key,
			Company:  source.Company,
			Status:   sourcePlanned,
		}
	}

	var mu sync.Mutex
	update := func(index int, run SourceRun) {
		mu.Lock()
		runs[index] = run
		mu.Unlock()
	}

	jobs := make([]internal.JobsFunc, 0, len(sources))
	for i, source := range sources {
		jobs = append(jobs, observeSource(source, logger, func(run SourceRun) {
			update(i, run)
		}))
	}

	results := func() []SourceRun {
		mu.Lock()
		defer mu.Unlock()

		return slices.Clone(runs)
	}

	return jobs, results
}

func observeSource(source Source, logger *slog.Logger, update func(SourceRun)) internal.JobsFunc {
	return func(ctx context.Context, client *http.Client) internal.Jobs {
		return func(yield func(*internal.JobPosting, error) bool) {
			run := SourceRun{
				Platform:  source.Platform,
				Key:       source.Key,
				Company:   source.Company,
				Status:    sourceRunning,
				StartedAt: time.Now().UTC(),
			}
			update(run)

			logger.LogAttrs(ctx, slog.LevelInfo, "source.start",
				slog.String("platform", source.Platform),
				slog.String("company", source.Company),
				slog.String("source_key", source.Key),
			)

			exhausted := false
			defer func() {
				if recovered := recover(); recovered != nil {
					run.Errors++
					run.ErrorClass = "panic"
					finishSourceRun(ctx, logger, &run, sourceFailed, update)
					panic(recovered)
				}

				status := sourceComplete
				switch {
				case ctx.Err() != nil:
					status = sourceTruncated
					run.ErrorClass = classifySourceError(ctx.Err())
				case !exhausted:
					status = sourceStopped
				case run.Errors > 0:
					status = sourceFailed
				}

				finishSourceRun(ctx, logger, &run, status, update)
			}()

			for posting, err := range source.Jobs(ctx, client) {
				if posting != nil {
					run.Postings++
				}
				if err != nil {
					run.Errors++
					run.ErrorClass = classifySourceError(err)
				}

				if !yield(posting, err) {
					return
				}
			}

			exhausted = true
		}
	}
}

func finishSourceRun(
	ctx context.Context,
	logger *slog.Logger,
	run *SourceRun,
	status string,
	update func(SourceRun),
) {
	run.Status = status
	run.FinishedAt = time.Now().UTC()
	run.DurationMS = run.FinishedAt.Sub(run.StartedAt).Milliseconds()
	update(*run)

	logger.LogAttrs(ctx, slog.LevelInfo, "source.finish",
		slog.String("platform", run.Platform),
		slog.String("company", run.Company),
		slog.String("source_key", run.Key),
		slog.String("status", run.Status),
		slog.Int("postings", run.Postings),
		slog.Int("errors", run.Errors),
		slog.String("error_class", run.ErrorClass),
		slog.Int64("duration_ms", run.DurationMS),
	)
}

func classifySourceError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	return fmt.Sprintf("%T", err)
}
