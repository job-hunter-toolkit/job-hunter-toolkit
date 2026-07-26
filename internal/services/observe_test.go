package services

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func TestObserveRecordsSourceOutcomes(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("board failed")
	sources := []Source{
		{
			Platform: "alpha",
			Key:      "acme-key",
			Company:  "acme",
			Jobs: func(context.Context, *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					yield(&internal.JobPosting{Company: "acme"}, nil)
				}
			},
		},
		{
			Platform: "beta",
			Key:      "globex-key",
			Company:  "globex",
			Jobs: func(context.Context, *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					yield(nil, wantErr)
				}
			},
		},
	}

	jobs, results := Observe(sources, nil)
	for range internal.AllWithConcurrency(t.Context(), nil, 2, jobs...) {
	}

	got := results()
	if len(got) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(got))
	}

	if got[0].Status != sourceComplete || got[0].Postings != 1 || got[0].Errors != 0 {
		t.Errorf("complete result = %+v, want complete with one posting", got[0])
	}
	if got[1].Status != sourceFailed || got[1].Postings != 0 || got[1].Errors != 1 {
		t.Errorf("failed result = %+v, want failed with one error", got[1])
	}
	if got[0].StartedAt.IsZero() || got[0].FinishedAt.IsZero() {
		t.Errorf("complete timestamps = %v to %v, want both populated", got[0].StartedAt, got[0].FinishedAt)
	}
}

func TestObserveRecordsNotStartedSource(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	sources := []Source{
		{
			Platform: "alpha",
			Key:      "acme",
			Company:  "acme",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					yield(nil, ctx.Err())
				}
			},
		},
	}

	jobs, results := Observe(sources, nil)
	for range internal.AllWithConcurrency(ctx, nil, 1, jobs...) {
	}

	got := results()
	if got[0].Status != sourcePlanned {
		t.Errorf("Status = %q, want %q when cancellation prevents a source starting", got[0].Status, sourcePlanned)
	}
}

func TestObserveRecordsTruncatedSource(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	sources := []Source{
		{
			Platform: "alpha",
			Key:      "acme",
			Company:  "acme",
			Jobs: func(ctx context.Context, _ *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					close(started)
					<-ctx.Done()
					yield(nil, ctx.Err())
				}
			},
		},
	}

	jobs, results := Observe(sources, nil)
	go func() {
		<-started
		cancel()
	}()

	for range internal.AllWithConcurrency(ctx, nil, 1, jobs...) {
	}

	got := results()
	if got[0].Status != sourceTruncated {
		t.Errorf("Status = %q, want %q", got[0].Status, sourceTruncated)
	}
	if got[0].ErrorClass != "canceled" {
		t.Errorf("ErrorClass = %q, want canceled", got[0].ErrorClass)
	}
}
