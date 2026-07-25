package services

import (
	"context"
	"iter"
	"net/http"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/tests"
	"github.com/picatz/iters"
	"github.com/shoenig/test/must"
)

// Every test that goes through these helpers reaches real third-party job
// boards, so they are gated behind [tests.RequireNetwork]. Hermetic coverage of
// the adapters lives in adapters_test.go.

type jobFunc func(ctx context.Context, httpClient *http.Client, company string) internal.Jobs

func testMultipleParallel(t *testing.T, companies iter.Seq[string], jobFn jobFunc) {
	tests.RequireNetwork(t)

	for company := range companies {
		t.Run(company, func(t *testing.T) {
			t.Parallel()

			testSingle(t, company, jobFn)
		})
	}
}

func testSingle(t *testing.T, company string, jobFn jobFunc) {
	tests.RequireNetwork(t)

	// Use the project's retrying client rather than http.DefaultClient, so a
	// single rate-limited response does not fail the check.
	client := httpx.NewClient()

	total := iters.Reduce2(
		jobFn(t.Context(), client, company),
		func(acc int, jobPosting *internal.JobPosting, err error) int {
			must.NoError(t, err)
			tests.CheckJobPosting(t, jobPosting)
			return acc + 1
		},
		0,
	)

	t.Logf("found %d job postings for %q", total, company)
}
