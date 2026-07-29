package jobposting_test

import (
	"fmt"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// A posting carries the board's own answers where it published them, and falls
// back to heuristics where it did not.
func ExampleJobPosting() {
	posting := &jobposting.JobPosting{
		Company:  "Oxide Computer",
		URL:      "https://jobs.example/oxide/1",
		Title:    "Senior Security Engineer",
		Location: "Remote - US",
		Compensation: &jobposting.Compensation{
			Min:        180_000,
			Max:        220_000,
			Currency:   "USD",
			Period:     jobposting.PeriodYear,
			Provenance: jobposting.ProvenanceEmployer,
		},
		Source: jobposting.PostingSource{Platform: "greenhouse", Key: "oxidecomputer"},
	}

	// No structured remote flag was published, so this reads the location text.
	fmt.Println("remote:", posting.IsRemote())

	top, ok := posting.Compensation.AnnualMax()
	fmt.Println("annual max:", top, ok)

	// Output:
	// remote: true
	// annual max: 220000 true
}

// An hourly range annualizes so it can be compared with a salaried one, which
// is what a single pay floor across both needs.
func ExampleCompensation_AnnualMin() {
	hourly := &jobposting.Compensation{Min: 22.50, Max: 28, Period: jobposting.PeriodHour}

	bottom, _ := hourly.AnnualMin()
	top, _ := hourly.AnnualMax()

	fmt.Println(bottom, top)

	// Output: 46800 58240
}

// Adapters map their platform's spelling onto the canonical vocabulary at the
// point of decoding. An unrecognised value reports false and must be left
// empty rather than guessed at.
func ExampleNormalizeEmploymentType() {
	for _, raw := range []string{"Full-Time", "FULL_TIME", "Intern (Summer 2026)", "Regular"} {
		typ, ok := jobposting.NormalizeEmploymentType(raw)
		fmt.Printf("%q -> %q %v\n", raw, typ, ok)
	}

	// Output:
	// "Full-Time" -> "full_time" true
	// "FULL_TIME" -> "full_time" true
	// "Intern (Summer 2026)" -> "internship" true
	// "Regular" -> "" false
}

// Postings are a [jobposting.Seq], so composing over a crawl is a range loop.
// Dedupe keys on URL and passes errors through untouched.
func ExampleDedupe() {
	var crawl jobposting.Seq = func(yield func(*jobposting.JobPosting, error) bool) {
		for _, posting := range []*jobposting.JobPosting{
			{Company: "acme", Title: "SRE", URL: "https://jobs.example/1"},
			{Company: "acme", Title: "SRE", URL: "https://jobs.example/1"},
			{Company: "acme", Title: "Security Engineer", URL: "https://jobs.example/2"},
		} {
			if !yield(posting, nil) {
				return
			}
		}
	}

	for posting, err := range jobposting.Dedupe(crawl) {
		if err != nil {
			fmt.Println("error:", err)
			continue
		}

		fmt.Println(posting.URL, posting.Title)
	}

	// Output:
	// https://jobs.example/1 SRE
	// https://jobs.example/2 Security Engineer
}
