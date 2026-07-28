package edgar_test

import (
	"context"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/edgar"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/enrichtest"
	"github.com/shoenig/test/must"
)

// tickersFixture is a trimmed company_tickers.json.
//
// The file is a JSON object keyed by a stringified row number rather than an
// array, and it publishes the CIK as a bare integer with no leading zeros. Both
// of those are the reason this package exists rather than a struct tag.
const tickersFixture = `{
  "0": {"cik_str": 320193, "ticker": "AAPL", "title": "Apple Inc."},
  "1": {"cik_str": 1477333, "ticker": "NET", "title": "Cloudflare, Inc."},
  "2": {"cik_str": 0, "ticker": "", "title": "Broken Row"}
}`

// submissionsFixture is a trimmed submissions document. The full document also
// carries every filing the entity has ever made, which this package deliberately
// ignores.
const submissionsFixture = `{
  "cik": "1477333",
  "entityType": "operating",
  "sic": "7372",
  "sicDescription": "Services-Prepackaged Software",
  "name": "Cloudflare, Inc.",
  "tickers": ["NET"],
  "exchanges": ["NYSE"],
  "stateOfIncorporation": "DE",
  "addresses": {"business": {"city": "San Francisco", "stateOrCountry": "CA"}},
  "formerNames": [{"name": "Cloudflare Inc"}],
  "filings": {"recent": {"form": ["10-K"]}}
}`

func TestCompanyTickers(t *testing.T) {
	t.Parallel()

	client, transport := enrichtest.Client(map[string]string{"company_tickers.json": tickersFixture})

	filers, err := edgar.CompanyTickers(t.Context(), client)
	must.NoError(t, err)

	// The zero-CIK row is dropped: a filer with no identifier cannot be joined
	// to anything, and keeping it would inflate the seed count.
	must.Len(t, 2, filers)

	// Sorted by CIK, so the generated table's diff is reviewable rather than
	// reordered by Go's map iteration on every run.
	must.Eq(t, "0000320193", filers[0].CIK)
	must.Eq(t, "0001477333", filers[1].CIK)
	must.Eq(t, "Cloudflare, Inc.", filers[1].Name)
	must.Eq(t, "NET", filers[1].Ticker)

	must.Len(t, 1, transport.Requests())
	must.StrContains(t, transport.Requests()[0], "www.sec.gov")
}

func TestSubmissions(t *testing.T) {
	t.Parallel()

	client, transport := enrichtest.Client(map[string]string{"submissions/CIK": submissionsFixture})

	submission, err := edgar.Submissions(t.Context(), client, "1477333")
	must.NoError(t, err)

	must.Eq(t, "0001477333", submission.CIK)
	must.Eq(t, "Cloudflare, Inc.", submission.Name)
	must.Eq(t, "7372", submission.SIC)
	must.Eq(t, "Services-Prepackaged Software", submission.SICDescription)
	must.Eq(t, "San Francisco, CA", submission.Business)
	must.Eq(t, []string{"NYSE"}, submission.Exchanges)
	must.Eq(t, []string{"Cloudflare Inc"}, submission.FormerNames)

	// The URL is built from the padded form; the unpadded one 404s.
	must.StrContains(t, transport.Requests()[0], "CIK0001477333.json")
}

// TestSubmissionsRefusesAMismatchedResponse is the guard against the worst
// failure this package could have: a redirect or a stale mirror answering with
// a different filer would otherwise write one company's industry onto another
// company's row, and the result would look entirely plausible.
func TestSubmissionsRefusesAMismatchedResponse(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(map[string]string{"submissions/CIK": submissionsFixture})

	_, err := edgar.Submissions(t.Context(), client, "320193")
	must.ErrorContains(t, err, "response describes CIK 0001477333")
}

// TestSubmissionsReportsAnUnexpectedStatus, including the body, because EDGAR
// explains a refusal there and "we are being throttled" and "the endpoint moved"
// need different fixes.
func TestSubmissionsReportsAnUnexpectedStatus(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(nil)

	_, err := edgar.Submissions(t.Context(), client, "320193")
	must.ErrorContains(t, err, "unexpected status")
	must.ErrorContains(t, err, "no fixture")
}

// TestSubmissionsAcceptsANumericSIC: EDGAR publishes the SIC as a string here
// and as a number elsewhere, and a decoder that insists on one breaks the day
// the other appears.
func TestSubmissionsAcceptsANumericSIC(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(map[string]string{
		"submissions/CIK": `{"cik": 1477333, "sic": 7372, "name": "Cloudflare, Inc."}`,
	})

	submission, err := edgar.Submissions(t.Context(), client, "0001477333")
	must.NoError(t, err)
	must.Eq(t, "7372", submission.SIC)
}

func TestNormalizeCIK(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"320193", "0000320193", "CIK0000320193"} {
		got, err := edgar.NormalizeCIK(raw)
		must.NoError(t, err)
		must.Eq(t, "0000320193", got, must.Sprintf("normalizing %q", raw))
	}

	_, err := edgar.NormalizeCIK("not-a-cik")
	must.Error(t, err)

	_, err = edgar.NormalizeCIK("")
	must.ErrorContains(t, err, "no digits")
}

// TestSubmissionsHonoursCancellation keeps the generator interruptible: it runs
// under a workflow timeout and a Ctrl-C must not leave it fetching.
func TestSubmissionsHonoursCancellation(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(map[string]string{"submissions/CIK": submissionsFixture})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := edgar.Submissions(ctx, client, "0001477333")
	must.ErrorIs(t, err, context.Canceled)
}

// multiTickerFixture is the shape the live file actually has, which the
// hermetic fixture above does not: one row per ticker, not one per filer, in
// the order SEC publishes them.
//
// Measured against https://www.sec.gov/files/company_tickers.json on
// 2026-07-28 — 10,432 rows, 8,017 distinct CIKs, 1,471 CIKs appearing more than
// once. These four filers, their tickers and their relative row order are copied
// from that file rather than invented; Alphabet really is four rows.
const multiTickerFixture = `{
  "0": {"cik_str": 1652044, "ticker": "GOOGL", "title": "Alphabet Inc."},
  "1": {"cik_str": 1652044, "ticker": "GOOG", "title": "Alphabet Inc."},
  "2": {"cik_str": 1652044, "ticker": "GOOGM", "title": "Alphabet Inc."},
  "3": {"cik_str": 1652044, "ticker": "GOOGN", "title": "Alphabet Inc."},
  "4": {"cik_str": 1166691, "ticker": "CMCSA", "title": "COMCAST CORP"},
  "5": {"cik_str": 1341439, "ticker": "ORCL", "title": "ORACLE CORP"},
  "6": {"cik_str": 1067983, "ticker": "BRK-B", "title": "BERKSHIRE HATHAWAY INC"},
  "7": {"cik_str": 1067983, "ticker": "BRK-A", "title": "BERKSHIRE HATHAWAY INC"},
  "8": {"cik_str": 1341439, "ticker": "ORCL-PD", "title": "ORACLE CORP"},
  "9": {"cik_str": 1166691, "ticker": "CCZ", "title": "COMCAST CORP"}
}`

// TestCompanyTickersCollapsesShareClasses pins the second bug the first live run
// exposed.
//
// resolve.Sources accepts a match only when a source proposes exactly one
// entity. While each ticker row became its own entity, every company with a
// second share class proposed two and was refused as "ambiguous" against itself
// — 22 correct matches lost and 44 review-queue rows whose stated reason named
// the entity they were already about.
func TestCompanyTickersCollapsesShareClasses(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(map[string]string{"company_tickers.json": multiTickerFixture})

	filers, err := edgar.CompanyTickers(t.Context(), client)
	must.NoError(t, err)

	must.Len(t, 4, filers)

	tickers := make(map[string]string, len(filers))
	for _, filer := range filers {
		tickers[filer.CIK] = filer.Ticker
	}

	// The earliest row wins, which is the primary listing in every case. Note
	// that three of these four contradict the shortest-ticker rule this started
	// out with: CCZ, GOOG and BRK-A are all shorter and all the wrong answer.
	must.Eq(t, "CMCSA", tickers["0001166691"])
	must.Eq(t, "GOOGL", tickers["0001652044"])
	must.Eq(t, "BRK-B", tickers["0001067983"])
	must.Eq(t, "ORCL", tickers["0001341439"])
}

// TestCompanyTickersIsIndependentOfMapOrder: the seed file decodes into a map,
// Go randomises map iteration, and the generator's output is reviewed as a diff.
// A collapse rule that read the file's order from iteration order rather than
// from the row-number keys would rewrite rows on every run.
func TestCompanyTickersIsIndependentOfMapOrder(t *testing.T) {
	t.Parallel()

	client, _ := enrichtest.Client(map[string]string{"company_tickers.json": multiTickerFixture})

	first, err := edgar.CompanyTickers(t.Context(), client)
	must.NoError(t, err)

	for range 20 {
		again, err := edgar.CompanyTickers(t.Context(), client)
		must.NoError(t, err)
		must.Eq(t, first, again)
	}
}
