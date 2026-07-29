package query_test

import (
	"errors"
	"fmt"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/query"
)

// crawl stands in for a real crawl: a sequence of postings that also carries
// the failure of one source.
var crawl jobposting.Seq = func(yield func(*jobposting.JobPosting, error) bool) {
	postings := []*jobposting.JobPosting{
		{
			Company:  "acme",
			URL:      "https://jobs.example/1",
			Title:    "Senior Security Engineer",
			Location: "Remote - US",
			Compensation: &jobposting.Compensation{
				Min: 180_000, Max: 220_000, Period: jobposting.PeriodYear,
			},
		},
		{
			Company:  "acme",
			URL:      "https://jobs.example/2",
			Title:    "Security Analyst",
			Location: "Austin, TX",
			Compensation: &jobposting.Compensation{
				Min: 90_000, Max: 110_000, Period: jobposting.PeriodYear,
			},
		},
		{
			Company:  "initech",
			URL:      "https://jobs.example/3",
			Title:    "Staff Backend Engineer",
			Location: "Remote (Worldwide)",
		},
	}

	for _, posting := range postings {
		if !yield(posting, nil) {
			return
		}
	}

	yield(nil, errors.New("hooli: unexpected status 503 Service Unavailable"))
}

// Fields are AND-ed across, OR-ed within. A pay floor necessarily also requires
// published pay, so postings that disclose nothing are excluded.
func ExampleQuery_Apply() {
	q := query.Query{
		Titles:    []string{"security", "appsec"},
		Remote:    true,
		MinAnnual: 150_000,
	}

	for posting, err := range q.Apply(crawl) {
		if err != nil {
			// Errors pass through, so a filtered crawl still reports which
			// sources failed.
			fmt.Println("error:", err)
			continue
		}

		fmt.Println(posting.Company, "-", posting.Title)
	}

	// Output:
	// acme - Senior Security Engineer
	// error: hooli: unexpected status 503 Service Unavailable
}

// The zero value matches everything, and says so, which lets a caller skip
// filtering entirely.
func ExampleQuery_IsZero() {
	fmt.Println(query.Query{}.IsZero())
	fmt.Println(query.Query{Titles: []string{"  "}}.IsZero())
	fmt.Println(query.Query{Titles: []string{"security"}}.IsZero())

	// Output:
	// true
	// true
	// false
}

// Match is the predicate on its own, for callers holding a single posting.
func ExampleQuery_Match() {
	q := query.Query{WorkplaceTypes: []jobposting.WorkplaceType{jobposting.WorkplaceTypeRemote}}

	// The board published nothing structured, so this falls back to the
	// location heuristic.
	fmt.Println(q.Match(&jobposting.JobPosting{Title: "SRE", Location: "Remote - US"}))

	// The board's own answer wins outright when it published one.
	fmt.Println(q.Match(&jobposting.JobPosting{
		Title:         "SRE",
		Location:      "Remote - US",
		WorkplaceType: jobposting.WorkplaceTypeHybrid,
	}))

	// Output:
	// true
	// false
}
