package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/shoenig/test/must"
	"github.com/spf13/cobra"
)

// runCompany executes the company command and returns its streams separately,
// which is the point: docs/architecture-roadmap.md requires structured data on
// stdout and diagnostics on stderr, and coverage is a diagnostic.
func runCompany(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	cmd := newCompanyCommand()

	var out, errOut bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)

	err = cmd.ExecuteContext(t.Context())

	return out.String(), errOut.String(), err
}

// TestCompanyCommandReportsCoverageOnStderr covers the honest-empty case, which
// is the state this feature ships in: the table has no rows until a generator
// run against the live sources happens, and "no output" has to be
// distinguishable from "this company is private".
func TestCompanyCommandReportsCoverageOnStderr(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runCompany(t)
	must.NoError(t, err)

	table, err := enrich.Default()
	must.NoError(t, err)

	if table.Len() == 0 {
		must.Eq(t, "", stdout, must.Sprint(
			"an empty table must print nothing to stdout, so a JSON pipeline reads an empty stream rather than prose"))
	}

	must.StrContains(t, stderr, "have a reviewed record")
	must.StrContains(t, stderr, "in the table overall")
}

// TestCompanyCommandRejectsTermsThatMatchNoSource uses the same wording the
// postings command does, because it resolves companies with the same function.
func TestCompanyCommandRejectsTermsThatMatchNoSource(t *testing.T) {
	t.Parallel()

	_, _, err := runCompany(t, "definitely-not-a-company-in-this-registry")
	must.ErrorContains(t, err, "no known companies match")
}

// TestCompanyCommandListsUnknownSources: the absence of a record is the common
// case, so "which of my shortlist do I have no context for" is a first-class
// question rather than an inference from silence.
func TestCompanyCommandListsUnknownSources(t *testing.T) {
	t.Parallel()

	stdout, _, err := runCompany(t, "--unknown", "cloudflare")
	must.NoError(t, err)
	must.StrContains(t, stdout, "cloudflare")

	// One line per source, "company<TAB>platform/key", so it pipes into cut.
	for line := range strings.Lines(strings.TrimSpace(stdout)) {
		must.StrContains(t, line, "\t")
		must.StrContains(t, line, "/")
	}
}

// TestCompanyCommandUnknownJSONIsSourceIdentity keeps the machine-readable
// answer keyed by platform and key rather than by a display name, matching what
// the table joins on.
func TestCompanyCommandUnknownJSONIsSourceIdentity(t *testing.T) {
	t.Parallel()

	stdout, _, err := runCompany(t, "--unknown", "--json", "cloudflare")
	must.NoError(t, err)

	for line := range strings.Lines(strings.TrimSpace(stdout)) {
		var source internal.PostingSource

		must.NoError(t, json.Unmarshal([]byte(line), &source))
		must.NotEq(t, "", source.Platform)
		must.NotEq(t, "", source.Key)
	}
}

// TestCompanyTextOmitsUnknownFields: an empty cell is read as absent by every
// reader, whereas "0 employees" or "not public" is a fact somebody would have
// had to establish.
func TestCompanyTextOmitsUnknownFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	must.NoError(t, writeEmployersText(&buf, []*enrich.Employer{{
		Source:  internal.PostingSource{Platform: "ashbyhq", Key: "beta"},
		Company: "beta",
		Match: enrich.Match{
			Method:      enrich.MethodManual,
			Confidence:  enrich.ConfidenceHigh,
			DataSources: []string{"wikidata"},
			RetrievedAt: "2026-07-27",
		},
	}}))

	output := buf.String()

	must.StrContains(t, output, "ashbyhq/beta")
	must.StrContains(t, output, "manual, high confidence, via wikidata, retrieved 2026-07-27")
	must.StrNotContains(t, output, "employees")
	must.StrNotContains(t, output, "public")
	must.StrNotContains(t, output, "cik")
}

// TestPublicTextKeepsUnknownDistinctFromNo is the tri-state at the surface. A
// company nobody resolved is not a private company.
func TestPublicTextKeepsUnknownDistinctFromNo(t *testing.T) {
	t.Parallel()

	yes, no := true, false

	must.Eq(t, "", publicText(nil))
	must.Eq(t, "yes", publicText(&yes))
	must.Eq(t, "no", publicText(&no))
}

// TestWageBenchmarkLineDisclaimsItself.
//
// docs/compensation.md states that nothing blends sources, and a benchmark is a
// third party's statement about somebody else's application or about an
// occupation in a metro. If it is ever shown next to a company it has to say so
// on the same line, because that is the only line a reader sees.
func TestWageBenchmarkLineDisclaimsItself(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	must.NoError(t, writeEmployersText(&buf, []*enrich.Employer{{
		Source: internal.PostingSource{Platform: "greenhouse", Key: "acme"},
		Match:  enrich.Match{Method: enrich.MethodManual, Confidence: enrich.ConfidenceHigh},
		WageBenchmarks: []enrich.WageBenchmark{{
			SOC: "15-1212", Occupation: "Information Security Analysts",
			Area: "CA", Source: enrich.WageSourceOFLC, N: 42,
			P25: 150000, P50: 175000, P75: 205000, AsOf: "FY2025",
		}},
	}}))

	must.StrContains(t, buf.String(), "NOT this employer's published pay")
}

// newEnrichedTestCommand builds a command carrying the postings enrichment
// flags, standing in for the wiring newPostingsCommand will do.
func newEnrichedTestCommand() (*cobra.Command, func(internal.Jobs) (internal.Jobs, error)) {
	cmd := &cobra.Command{Use: "postings", RunE: func(*cobra.Command, []string) error { return nil }}

	return cmd, registerEnrichmentFlags(cmd)
}

// TestEnrichmentIsIdentityWhenNoFilterIsGiven is the compatibility guarantee
// for the default crawl: same stream, same postings, same order, no table load.
func TestEnrichmentIsIdentityWhenNoFilterIsGiven(t *testing.T) {
	t.Parallel()

	_, decorate := newEnrichedTestCommand()

	original := &internal.JobPosting{Company: "acme", URL: "https://example.test/1"}

	jobs, err := decorate(func(yield func(*internal.JobPosting, error) bool) {
		yield(original, nil)
	})
	must.NoError(t, err)

	var found []*internal.JobPosting
	for job, err := range jobs {
		must.NoError(t, err)

		found = append(found, job)
	}

	must.Len(t, 1, found)
	must.Eq(t, original, found[0])
}

// TestEnrichmentFilterOnAnEmptyTableFailsLoudly.
//
// With no rows compiled in, an employer filter would discard every posting, and
// zero postings reads as "nothing is hiring" rather than "this binary has no
// employer data". Same reasoning internal/filter.go applies to a blank --title.
func TestEnrichmentFilterOnAnEmptyTableFailsLoudly(t *testing.T) {
	t.Parallel()

	table, err := enrich.Default()
	must.NoError(t, err)

	if table.Len() > 0 {
		t.Skip("the committed table has rows, so an employer filter is answerable")
	}

	cmd, decorate := newEnrichedTestCommand()
	must.NoError(t, cmd.Flags().Set("known-employer", "true"))

	_, err = decorate(func(func(*internal.JobPosting, error) bool) {})
	must.ErrorContains(t, err, "no employer records are compiled into this binary")
	must.ErrorContains(t, err, "tools/enrichgen")
}

// TestPublicAndPrivateAreMutuallyExclusive: both together match nothing while
// looking like a filter that worked, so cobra refuses the combination.
func TestPublicAndPrivateAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	cmd, _ := newEnrichedTestCommand()

	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--public", "--private"})

	err := cmd.ExecuteContext(t.Context())
	must.ErrorContains(t, err, "none of the others can be")
}

// TestPostingsCommandCarriesTheEnrichmentFlags guards the wiring in
// newPostingsCommand.
//
// This test previously asserted the opposite, that postings did NOT define these
// flags, because it was written while enrichment lived in a file that could not
// touch main.go and the assertion it wanted was "registering these later will
// not collide". Now that they are registered, the inverse is the property worth
// holding: cobra panics on a duplicate flag name rather than returning an error,
// so a second registration would take the binary down on startup, and a dropped
// registration would make every employer filter silently unavailable.
func TestPostingsCommandCarriesTheEnrichmentFlags(t *testing.T) {
	t.Parallel()

	postings := newPostingsCommand()

	for _, name := range []string{"known-employer", "industry", "public", "private", "min-employees"} {
		must.NotNil(t, postings.Flags().Lookup(name), must.Sprintf(
			"the postings command is missing --%s, so registerEnrichmentFlags is no longer wired in", name))
	}

	// Building it twice must not panic: the root command is constructed per
	// invocation, and a flag registered on a package-level flag set rather than
	// on the command would fail the second time round.
	must.NotNil(t, newPostingsCommand())
}
