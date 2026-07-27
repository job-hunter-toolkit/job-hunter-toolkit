package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/enrichtest"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/gen"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/resolve"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/shoenig/test/must"
)

// The fixtures below use invented companies on purpose.
//
// A fixture carrying a real company's name and an invented CIK is one careless
// copy away from being committed as data, and a fabricated identifier is
// indistinguishable from a real one once it is in the table. Everything here is
// obviously fictional.
const (
	tickersFixture = `{
  "0": {"cik_str": 1000001, "ticker": "EXR", "title": "Example Robotics, Inc."},
  "1": {"cik_str": 1000002, "ticker": "ATL", "title": "Atlas Example Corp"},
  "2": {"cik_str": 1000003, "ticker": "ATS", "title": "Atlas Example Inc."}
}`

	robotics = `{
  "cik": "1000001",
  "name": "Example Robotics, Inc.",
  "sic": "3559",
  "sicDescription": "Special Industry Machinery",
  "tickers": ["EXR"],
  "exchanges": ["NASDAQ"],
  "addresses": {"business": {"city": "Portland", "stateOrCountry": "OR"}}
}`

	sparql = `{"results": {"bindings": [
  {
    "item": {"value": "http://www.wikidata.org/entity/Q1000001"},
    "itemLabel": {"value": "Example Robotics"},
    "cik": {"value": "0001000001"},
    "employees": {"value": "2400"},
    "employeesAsOf": {"value": "2025-06-30T00:00:00Z"},
    "inception": {"value": "2011-02-03T00:00:00Z"},
    "parentLabel": {"value": "Example Holding NV"}
  }
]}}`
)

// fixtureSources are the crawled boards the generator resolves. They cover the
// three outcomes that matter: a clean match, an ambiguous one, and a company
// EDGAR has never heard of.
var fixtureSources = []resolve.Source{
	{Platform: "greenhouse", Key: "examplerobotics", Company: "examplerobotics"},
	{Platform: "ashbyhq", Key: "atlasexample", Company: "atlasexample"},
	{Platform: "lever", Key: "somestartup", Company: "somestartup"},
}

func fixtureClients(t *testing.T) gen.Options {
	t.Helper()

	edgarClient, _ := enrichtest.Client(map[string]string{
		"company_tickers.json": tickersFixture,
		"CIK0001000001.json":   robotics,
		"CIK0001000002.json":   `{"cik": "1000002", "name": "Atlas Example Corp"}`,
		"CIK0001000003.json":   `{"cik": "1000003", "name": "Atlas Example Inc."}`,
	})

	wikidataClient, _ := enrichtest.Client(map[string]string{"query.wikidata.org": sparql})

	return gen.Options{
		EDGAR:    edgarClient,
		Wikidata: wikidataClient,
		Now:      time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
	}
}

// TestRunResolvesFetchesAndDecorates walks the whole pipeline: seed file,
// offline resolution, per-match submissions, Wikidata decoration by CIK.
func TestRunResolvesFetchesAndDecorates(t *testing.T) {
	t.Parallel()

	opts := fixtureClients(t)
	opts.Sources = fixtureSources

	result, err := gen.Run(t.Context(), opts)
	must.NoError(t, err)

	must.Len(t, 1, result.Employers, must.Sprint(
		"only the unambiguous match may be committed"))

	employer := result.Employers[0]
	must.Eq(t, "greenhouse", employer.Source.Platform)
	must.Eq(t, "Example Robotics, Inc.", employer.LegalName)
	must.Eq(t, "0001000001", employer.CIK)
	must.Eq(t, "3559", employer.SIC)
	must.Eq(t, "Special Industry Machinery", employer.Industry)
	must.Eq(t, "Portland, OR", employer.Headquarters)
	must.Eq(t, "NASDAQ", employer.Exchange)
	must.NotNil(t, employer.Public)
	must.True(t, *employer.Public, must.Sprint(
		"an entity in company_tickers.json trades under a ticker, so public is derived rather than assumed"))

	// Wikidata joined by CIK, never by name.
	must.Eq(t, "Q1000001", employer.WikidataID)
	must.Eq(t, 2400, employer.Employees)
	must.Eq(t, 2011, employer.Founded)
	must.Eq(t, "Example Holding NV", employer.Parent)

	must.Eq(t, []string{"sec-edgar", "wikidata"}, employer.Match.DataSources)
	must.Eq(t, "2026-07-27", employer.Match.RetrievedAt)
	must.Eq(t, enrich.ConfidenceHigh, employer.Match.Confidence)

	// The ambiguous company produced review rows rather than a guess, and the
	// company EDGAR does not know produced nothing at all.
	must.Len(t, 2, result.Candidates)
	for _, candidate := range result.Candidates {
		must.Eq(t, "ashbyhq", candidate.Source.Platform)
	}

	must.Eq(t, 3, result.Stats.Sources)
	must.Eq(t, 1, result.Stats.Matched)
	must.Eq(t, 1, result.Stats.SubmissionsOK)
	must.Eq(t, 0, result.Stats.SubmissionsFail)
	must.Eq(t, 1, result.Stats.WikidataMatched)
}

// TestWriteTablesRoundTripsThroughTheLoader is the test that ties the generator
// to the binary. A generator that emits a table the loader rejects is a failure
// that would otherwise appear only after a live run in CI.
func TestWriteTablesRoundTripsThroughTheLoader(t *testing.T) {
	t.Parallel()

	opts := fixtureClients(t)
	opts.Sources = fixtureSources

	result, err := gen.Run(t.Context(), opts)
	must.NoError(t, err)

	// Laid out exactly as the embedded filesystem is, so the same loader reads
	// it: internal/enrich/data/*.tsv.
	root := t.TempDir()
	dir := filepath.Join(root, "data")
	must.NoError(t, os.MkdirAll(dir, 0o755))
	must.NoError(t, result.WriteTables(dir, opts.Now))

	// The loader wants all three files. manual.tsv and wages.tsv are never
	// written by a run — the first belongs to humans, the second has no
	// generator yet — so they are supplied here as the repository carries them.
	must.NoError(t, os.WriteFile(filepath.Join(dir, "manual.tsv"),
		[]byte(strings.Join(enrich.EmployerColumns(), "\t")+"\n"), 0o644))
	must.NoError(t, os.WriteFile(filepath.Join(dir, "wages.tsv"),
		[]byte(strings.Join(enrich.WageColumns(), "\t")+"\n"), 0o644))

	loaded, err := enrich.LoadFS(os.DirFS(root))
	must.NoError(t, err, must.Sprint("the generated table must load with the parser the binary uses"))
	must.Eq(t, 1, loaded.Len())

	employer, ok := loaded.For(internal.PostingSource{Platform: "greenhouse", Key: "examplerobotics"})
	must.True(t, ok)
	must.Eq(t, "Example Robotics, Inc.", employer.LegalName)
	must.Eq(t, 2400, employer.Employees)

	// The review queue is written for a person, and its whole value is the
	// reason column.
	candidates, err := os.ReadFile(filepath.Join(dir, gen.CandidatesFile))
	must.NoError(t, err)
	must.StrContains(t, string(candidates), "ambiguous")
	must.StrContains(t, string(candidates), "Atlas Example")
	must.StrContains(t, string(candidates), "manual.tsv")
}

// TestGeneratedTableIsDeterministic: the output is reviewed as a diff, so two
// runs over the same data must produce identical bytes. Map iteration order
// would otherwise present every row as changed on every monthly refresh.
func TestGeneratedTableIsDeterministic(t *testing.T) {
	t.Parallel()

	render := func() string {
		opts := fixtureClients(t)
		opts.Sources = fixtureSources

		result, err := gen.Run(t.Context(), opts)
		must.NoError(t, err)

		dir := t.TempDir()
		must.NoError(t, result.WriteTables(dir, opts.Now))

		content, err := os.ReadFile(filepath.Join(dir, gen.EmployersFile))
		must.NoError(t, err)

		return string(content)
	}

	must.Eq(t, render(), render())
}

// TestRunRefusesToCommitABlockedRun is the integrity guard.
//
// A run whose submission fetches were all refused — the shape of an IP block or
// a moved endpoint — would otherwise write a full set of rows with every
// industry blank. That table looks like data and reads like an answer, so the
// run fails instead.
func TestRunRefusesToCommitABlockedRun(t *testing.T) {
	t.Parallel()

	edgarClient, _ := enrichtest.Client(map[string]string{"company_tickers.json": tickersFixture})

	_, err := gen.Run(t.Context(), gen.Options{
		EDGAR:   edgarClient,
		Sources: fixtureSources,
		Now:     time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
	})

	must.ErrorContains(t, err, "refusing to write enrichment tables")
	must.ErrorContains(t, err, "looks like a block")
}

// TestRunWithoutAnEDGARClientIsRefused: an unidentified, unpaced client is the
// request SEC blocks, so there is no default.
func TestRunWithoutAnEDGARClientIsRefused(t *testing.T) {
	t.Parallel()

	_, err := gen.Run(t.Context(), gen.Options{})
	must.ErrorContains(t, err, "no EDGAR client")
}

// TestWikidataIsOptional lets a refresh update SEC facts alone, which is the
// cheaper and more frequent half.
func TestWikidataIsOptional(t *testing.T) {
	t.Parallel()

	opts := fixtureClients(t)
	opts.Sources = fixtureSources
	opts.Wikidata = nil

	result, err := gen.Run(t.Context(), opts)
	must.NoError(t, err)
	must.Len(t, 1, result.Employers)
	must.Eq(t, 0, result.Employers[0].Employees)
	must.Eq(t, []string{"sec-edgar"}, result.Employers[0].Match.DataSources)
}

// TestSourcesFromKeepsTheCrawlerIdentity checks the conversion from the real
// registry preserves platform and key, which are the join key everything else
// depends on.
func TestSourcesFromKeepsTheCrawlerIdentity(t *testing.T) {
	t.Parallel()

	converted := gen.SourcesFrom(services.Builtin)
	must.SliceNotEmpty(t, converted)
	must.Eq(t, len(services.Builtin), len(converted))

	for i, source := range converted {
		must.Eq(t, services.Builtin[i].Platform, source.Platform)
		must.Eq(t, services.Builtin[i].Key, source.Key)
		must.Eq(t, services.Builtin[i].Company, source.Company)
	}
}
