package companies

import (
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/tests"
	"github.com/shoenig/test/must"
)

func TestOxide(t *testing.T) {
	t.Parallel()
	tests.RequireNetwork(t)

	var found int

	for jobPosting, err := range Oxide(t.Context(), httpx.NewClient()) {
		must.NoError(t, err)
		tests.CheckJobPosting(t, jobPosting)

		found++
	}

	t.Logf("found %d job postings for Oxide", found)
}
