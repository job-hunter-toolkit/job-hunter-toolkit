package enrich_test

import (
	"encoding/json"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/shoenig/test/must"
)

// employer builds a table row for a test.
func employer(platform, key, name string) *enrich.Employer {
	public := true

	return &enrich.Employer{
		Source:    internal.PostingSource{Platform: platform, Key: key},
		Company:   key,
		LegalName: name,
		CIK:       "0000000001",
		SIC:       "7372",
		Industry:  "Services-Prepackaged Software",
		Public:    &public,
		Employees: 4000,
		Match: enrich.Match{
			Method:      enrich.MethodEDGARExactName,
			Confidence:  enrich.ConfidenceHigh,
			DataSources: []string{"sec-edgar"},
			RetrievedAt: "2026-07-27",
		},
	}
}

// posting builds a posting from one source.
func posting(platform, key, company string) *internal.JobPosting {
	return &internal.JobPosting{
		Company: company,
		URL:     "https://example.test/jobs/1",
		Title:   "Security Engineer",
		Source:  internal.PostingSource{Platform: platform, Key: key},
	}
}

// collect drains an enriched stream.
func collect(t *testing.T, postings enrich.Postings) ([]*enrich.Posting, []error) {
	t.Helper()

	var (
		found  []*enrich.Posting
		failed []error
	)

	for p, err := range postings {
		if err != nil {
			failed = append(failed, err)

			continue
		}

		found = append(found, p)
	}

	return found, failed
}

// jobsFrom turns fixed postings and errors into a stream.
func jobsFrom(items ...any) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		for _, item := range items {
			var (
				job *internal.JobPosting
				err error
			)

			switch value := item.(type) {
			case *internal.JobPosting:
				job = value
			case error:
				err = value
			}

			if !yield(job, err) {
				return
			}
		}
	}
}

// TestPostingJSONIsAdditive is the contract test for the output format.
//
// The README advertises piping `postings --json` into jq, and jobs_record.txt
// plus the nightly workflow consume this binary's output. An enriched posting
// must therefore serialize to exactly the bytes the underlying posting already
// produced, plus an `employer` key that is absent when there is no employer.
// Reading the struct tags and believing them is not enough: embedding a pointer
// changes how encoding/json walks the value, and this asserts the result.
func TestPostingJSONIsAdditive(t *testing.T) {
	t.Parallel()

	job := posting("greenhouse", "acme", "Acme")

	plain, err := json.Marshal(job)
	must.NoError(t, err)

	unenriched, err := json.Marshal(&enrich.Posting{JobPosting: job})
	must.NoError(t, err)

	must.Eq(t, string(plain), string(unenriched), must.Sprint(
		"a posting with no employer must serialize byte-for-byte as it does today"))

	enriched, err := json.Marshal(&enrich.Posting{JobPosting: job, Employer: employer("greenhouse", "acme", "Acme, Inc.")})
	must.NoError(t, err)

	var decoded map[string]json.RawMessage
	must.NoError(t, json.Unmarshal(enriched, &decoded))

	var plainKeys map[string]json.RawMessage
	must.NoError(t, json.Unmarshal(plain, &plainKeys))

	for key, value := range plainKeys {
		must.MapContainsKey(t, decoded, key)
		must.Eq(t, string(value), string(decoded[key]), must.Sprintf("enrichment changed the %q field", key))
	}

	must.MapContainsKey(t, decoded, "employer")
	must.Eq(t, len(plainKeys)+1, len(decoded), must.Sprint("enrichment added more than the employer key"))
}

// TestAttachJoinsOnSourceIdentityNotCompanyName is the test that keeps this
// feature honest.
//
// Two different employers can carry the same display name: Phenom and Workday
// derive it from a hostname or tenant URL, and services.Companies deduplicates
// it case-insensitively. Joining on it would attach one company's headcount to
// another, which is a plausible wrong answer nobody would ever spot.
func TestAttachJoinsOnSourceIdentityNotCompanyName(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(
		employer("greenhouse", "acme", "Acme Industries, Inc."),
		employer("workday", "https://acme.wd1.myworkdayjobs.com/careers", "Acme Aerospace Holdings"),
	)

	found, failed := collect(t, table.Attach(jobsFrom(
		posting("greenhouse", "acme", "Acme"),
		posting("workday", "https://acme.wd1.myworkdayjobs.com/careers", "Acme"),
		posting("lever", "acme", "Acme"),
	)))

	must.SliceEmpty(t, failed)
	must.Len(t, 3, found)

	must.NotNil(t, found[0].Employer)
	must.Eq(t, "Acme Industries, Inc.", found[0].Employer.LegalName)

	must.NotNil(t, found[1].Employer)
	must.Eq(t, "Acme Aerospace Holdings", found[1].Employer.LegalName)

	must.Nil(t, found[2].Employer, must.Sprint(
		"a source with no reviewed row must get nothing, not the row of a company with the same display name"))
}

// TestAttachIgnoresPostingsWithNoSourceIdentity covers adapters that have not
// been migrated to stamp a PostingSource. They must get no employer rather than
// a name-matched guess.
func TestAttachIgnoresPostingsWithNoSourceIdentity(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(employer("greenhouse", "acme", "Acme Industries, Inc."))

	job := &internal.JobPosting{Company: "acme", URL: "https://example.test/1"}

	found, _ := collect(t, table.Attach(jobsFrom(job)))

	must.Len(t, 1, found)
	must.Nil(t, found[0].Employer)
}

// TestAttachPassesErrorsThrough guards the roadmap invariant that a failed
// source cannot make previously seen jobs look removed. Enrichment sits in the
// middle of the stream; swallowing an error there would erase a source's
// failure from the crawl's report.
func TestAttachPassesErrorsThrough(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(employer("greenhouse", "acme", "Acme Industries, Inc."))

	found, failed := collect(t, table.Attach(jobsFrom(
		posting("greenhouse", "acme", "Acme"),
		errBoardDown,
		posting("greenhouse", "acme", "Acme"),
	)))

	must.Len(t, 2, found)
	must.Len(t, 1, failed)
	must.ErrorIs(t, failed[0], errBoardDown)
}

// TestAttachStopsWhenTheConsumerStops checks the decorator honours an early
// break, which is what cancels an in-flight crawl.
func TestAttachStopsWhenTheConsumerStops(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable()

	yielded := 0

	for range table.Attach(jobsFrom(
		posting("greenhouse", "acme", "Acme"),
		posting("greenhouse", "acme", "Acme"),
		posting("greenhouse", "acme", "Acme"),
	)) {
		yielded++

		break
	}

	must.Eq(t, 1, yielded)
}

// TestFlattenRestoresThePlainStream covers the path a filtered `postings` run
// takes: enrich, filter, then hand the original posting shape to the printer.
func TestFlattenRestoresThePlainStream(t *testing.T) {
	t.Parallel()

	table := enrich.NewTable(employer("greenhouse", "acme", "Acme Industries, Inc."))
	original := posting("greenhouse", "acme", "Acme")

	var (
		found  []*internal.JobPosting
		failed []error
	)

	for job, err := range enrich.Flatten(table.Attach(jobsFrom(original, errBoardDown))) {
		if err != nil {
			failed = append(failed, err)

			continue
		}

		found = append(found, job)
	}

	must.Len(t, 1, found)
	must.Eq(t, original, found[0], must.Sprint("flattening must return the identical posting, not a copy"))
	must.Len(t, 1, failed)
}

// TestNilTableAnswersNothing: a caller that could not load a table still runs.
// No enrichment is the documented default, and refusing to print postings
// because a lookup table is missing would be a worse failure than the one it
// reports.
func TestNilTableAnswersNothing(t *testing.T) {
	t.Parallel()

	var table *enrich.Table

	employer, ok := table.For(internal.PostingSource{Platform: "greenhouse", Key: "acme"})
	must.False(t, ok)
	must.Nil(t, employer)
	must.Eq(t, 0, table.Len())
	must.SliceEmpty(t, table.All())
}
