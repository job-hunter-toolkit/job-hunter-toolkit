package companies

import (
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/tests"
	"github.com/shoenig/test/must"
)

func TestUber(t *testing.T) {
	t.Parallel()
	tests.RequireNetwork(t)

	jobPostings, err := Uber(t.Context(), httpx.NewClient())
	must.NoError(t, err)

	var found int

	for jobPosting := range jobPostings {
		tests.CheckJobPosting(t, jobPosting)

		found++
	}

	t.Logf("found %d job postings for Uber", found)
}
