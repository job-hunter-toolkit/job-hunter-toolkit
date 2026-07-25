// Package tests provides helpers shared by this project's tests.
package tests

import (
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test/must"
)

// T is the subset of [testing.T] that [CheckJobPosting] needs. Accepting an
// interface lets the checker itself be tested.
type T interface {
	Helper()
	Fatalf(string, ...any)
}

// CheckJobPosting asserts the invariants every job posting must satisfy.
//
// Only URL and Company are checked. Title and Location are deliberately not:
// some boards publish postings with an empty title, and plenty omit a location
// entirely. Those postings are still worth returning, because a link to a real
// opening is useful even when the board metadata is thin. Requiring those fields
// would mean discarding real jobs to satisfy a test.
func CheckJobPosting(t T, job *internal.JobPosting) {
	t.Helper()

	must.StrHasPrefix(t, "https://", job.URL, must.Sprintf("job URL %q is not valid", job.URL))
	must.NotEq(t, "", job.Company, must.Sprintf("job company is empty for %q", job.URL))
}
