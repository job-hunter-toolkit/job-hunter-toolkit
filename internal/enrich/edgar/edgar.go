// Package edgar reads the SEC's key-free company data.
//
// Two endpoints carry everything this project needs, both public, both
// unauthenticated, both canonical:
//
//   - https://www.sec.gov/files/company_tickers.json — roughly ten thousand
//     {cik, ticker, title} records, recompiled nightly. This is the seed: it is
//     the only cheap way to turn a set of company names into a set of CIKs
//     without asking EDGAR ten thousand questions.
//   - https://data.sec.gov/submissions/CIK##########.json — one filer's
//     registration facts: legal name, SIC industry, business address, tickers,
//     exchanges, former names.
//
// Everything EDGAR publishes is a US Government work in the public domain, so
// the derived table can be redistributed with the binary. What EDGAR asks for in
// return is stated in its access policy: a User-Agent that identifies the
// requester with a contact, and no more than 10 requests per second across all
// of its hosts. [fetch.Client] supplies both. Exceeding the rate earns a 403 and
// an IP block of roughly ten minutes, which in this project would land on the
// shared GitHub Actions runner rather than on a developer.
//
// This package is imported only by the generator. Nothing in the CLI's import
// graph reaches it.
package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/fetch"
)

// Host endpoints. Both are grouped under one rate budget by SEC, which is why
// the generator must drive them from a single paced client.
const (
	// TickersURL is the seed file mapping CIKs to tickers and names.
	TickersURL = "https://www.sec.gov/files/company_tickers.json"

	// SubmissionsURLPrefix is joined with a ten-digit CIK and ".json".
	SubmissionsURLPrefix = "https://data.sec.gov/submissions/CIK"

	// Interval is the minimum spacing between EDGAR requests.
	//
	// SEC documents 10 requests per second; this is 150ms, about 6.7/s. The
	// headroom is deliberate: the published limit is enforced per IP, a GitHub
	// Actions runner's IP is shared with every other workflow on that host, and
	// the cost of being slightly slow in a monthly job is nothing while the cost
	// of a block is a failed refresh and a bad neighbour.
	Interval = 150 * time.Millisecond
)

// Filer is one row of company_tickers.json.
type Filer struct {
	// CIK is zero-padded to ten digits, which is the form the submissions API
	// and Wikidata's P5531 both use. The source file publishes it as a bare
	// integer.
	CIK string

	// Ticker is the primary trading symbol.
	Ticker string

	// Name is the filer's name as EDGAR spells it.
	Name string
}

// tickerRecord is the on-the-wire shape of one company_tickers.json entry. The
// file is a JSON object keyed by a stringified row number rather than an array,
// so it decodes into a map.
type tickerRecord struct {
	CIK    flexInt `json:"cik_str"`
	Ticker string  `json:"ticker"`
	Title  string  `json:"title"`
}

// CompanyTickers fetches the seed file, one Filer per CIK.
//
// One request for every public filer, rather than one request per company this
// project crawls. At ~2,131 sources that is the difference between a single
// megabyte-sized download and 2,131 paced requests, which at EDGAR's rate limit
// would be more than five minutes of pure waiting before any useful work began.
//
// # One CIK, many rows
//
// The file is keyed by row number and carries one row per *ticker*, not per
// filer. Measured on 2026-07-28: 10,432 rows, 8,017 distinct CIKs. 1,471 CIKs
// appear more than once, because share classes, preferred series, warrants and
// ADRs each get their own row under the same CIK — CIK 1652044 is four rows
// (GOOGL, GOOG, GOOGM, GOOGN) and CIK 70858 is seventeen.
//
// Returning those as separate entities is not a cosmetic problem: [resolve]
// accepts a match only when a source proposes exactly one entity, so a company
// with two share classes proposed two and was refused as "ambiguous" against
// itself. That cost 22 correct matches in the first live run — Alphabet, Block,
// BuzzFeed, Aurora Innovation, GeneDx and Eve among them — and filled the review
// queue with 44 rows whose stated reason named the very entity they were about.
// So the rows are collapsed here, at the edge, rather than left for every
// consumer to rediscover.
func CompanyTickers(ctx context.Context, client *http.Client) ([]Filer, error) {
	var records map[string]tickerRecord

	if err := fetch.JSON(ctx, client, TickersURL, "application/json", &records); err != nil {
		return nil, fmt.Errorf("fetching SEC company tickers: %w", err)
	}

	byCIK := make(map[string]rankedFiler, len(records))

	for key, record := range records {
		if record.CIK == 0 {
			continue
		}

		candidate := rankedFiler{
			row: rowIndex(key),
			filer: Filer{
				CIK:    PadCIK(int(record.CIK)),
				Ticker: strings.TrimSpace(record.Ticker),
				Name:   strings.TrimSpace(record.Title),
			},
		}

		if existing, ok := byCIK[candidate.filer.CIK]; ok && !candidate.beats(existing) {
			continue
		}

		byCIK[candidate.filer.CIK] = candidate
	}

	filers := make([]Filer, 0, len(byCIK))
	for _, ranked := range byCIK {
		filers = append(filers, ranked.filer)
	}

	// Map iteration order is random, and the generator's output must be a
	// reviewable diff. Sorting by CIK also makes "which filer won a tie" a
	// property of the data rather than of the run.
	slices.SortFunc(filers, func(a, b Filer) int { return strings.Compare(a.CIK, b.CIK) })

	return filers, nil
}

// rankedFiler is one row of the seed file together with its position in it.
type rankedFiler struct {
	filer Filer
	row   int
}

// beats reports whether r is the row this project should keep for its CIK.
//
// The earliest row wins, and that is a real rule rather than an arbitrary one:
// SEC orders company_tickers.json so a filer's primary listing comes before its
// secondary classes, preferred series, warrants and F-share ADRs. Measured on
// the 2026-07-28 file, taking the first row yields CMCSA for Comcast, GOOGL for
// Alphabet, BRK-B for Berkshire, CAJPY for Canon and BZLFY for Bunzl — in each
// case the security a person means by the company's name.
//
// The obvious alternative, shortest ticker, was tried first and is wrong: it
// picks CCZ over CMCSA (a tracking security), CHSCL over CHSCP and CAJFF over
// CAJPY (illiquid F-shares over the traded ADR). The two rules disagree on 209
// of the 1,471 multi-ticker CIKs, so this is not a hypothetical difference.
//
// Ties go to the lower ticker so the choice never depends on map iteration
// order. The generator's output is reviewed as a diff, and a tie broken by
// iteration order would rewrite rows on every run for no reason.
func (r rankedFiler) beats(other rankedFiler) bool {
	if r.row != other.row {
		return r.row < other.row
	}

	return r.filer.Ticker < other.filer.Ticker
}

// rowIndex reads the position of a record from its key.
//
// company_tickers.json is a JSON object whose keys are stringified row numbers,
// so the key carries the file's ordering — which encoding/json otherwise throws
// away when it decodes into a map. A key that is not a number keeps its record
// but sorts last, because an unrecognised key is exactly the case where the
// ordering cannot be trusted to mean anything.
func rowIndex(key string) int {
	index, err := strconv.Atoi(strings.TrimSpace(key))
	if err != nil || index < 0 {
		return math.MaxInt
	}

	return index
}

// Submission is the subset of a filer's submissions document this project uses.
//
// Deliberately narrow. The document also carries every filing the entity has
// ever made, and an enrichment table that grew a filings history would stop
// being a lookup table and start being an archive, in a binary that is supposed
// to stay portable.
type Submission struct {
	CIK            string
	Name           string
	SIC            string
	SICDescription string
	Tickers        []string
	Exchanges      []string

	// Business is the business address as "City, ST", which is the closest
	// EDGAR comes to a headquarters. State of incorporation is deliberately not
	// used for this: nearly every filer is incorporated in Delaware, which
	// answers a legal question and no geographic one.
	Business string

	// FormerNames are previous legal names, kept because a company that renamed
	// itself is exactly the case where a board slug matches the old name.
	FormerNames []string
}

// submissionDocument is the on-the-wire shape.
type submissionDocument struct {
	CIK            flexString `json:"cik"`
	Name           string     `json:"name"`
	SIC            flexString `json:"sic"`
	SICDescription string     `json:"sicDescription"`
	Tickers        []string   `json:"tickers"`
	Exchanges      []string   `json:"exchanges"`

	Addresses struct {
		Business struct {
			City           string `json:"city"`
			StateOrCountry string `json:"stateOrCountry"`
		} `json:"business"`
	} `json:"addresses"`

	FormerNames []struct {
		Name string `json:"name"`
	} `json:"formerNames"`
}

// Submissions fetches one filer's registration facts.
func Submissions(ctx context.Context, client *http.Client, cik string) (*Submission, error) {
	padded, err := NormalizeCIK(cik)
	if err != nil {
		return nil, err
	}

	url := SubmissionsURLPrefix + padded + ".json"

	var document submissionDocument

	if err := fetch.JSON(ctx, client, url, "application/json", &document); err != nil {
		return nil, fmt.Errorf("fetching SEC submissions for CIK %s: %w", padded, err)
	}

	submission := &Submission{
		// The response's own CIK is not trusted over the one requested: a
		// redirect or a stale mirror answering with a different filer would
		// otherwise attach one company's industry to another's row, which is
		// precisely the failure this whole design is arranged to prevent.
		CIK:            padded,
		Name:           strings.TrimSpace(document.Name),
		SIC:            strings.TrimSpace(string(document.SIC)),
		SICDescription: strings.TrimSpace(document.SICDescription),
		Tickers:        document.Tickers,
		Exchanges:      document.Exchanges,
	}

	if got := strings.TrimSpace(string(document.CIK)); got != "" {
		if normalized, err := NormalizeCIK(got); err == nil && normalized != padded {
			return nil, fmt.Errorf("fetching SEC submissions for CIK %s: response describes CIK %s instead", padded, normalized)
		}
	}

	if city, state := document.Addresses.Business.City, document.Addresses.Business.StateOrCountry; city != "" || state != "" {
		submission.Business = strings.TrimSpace(strings.Trim(strings.TrimSpace(city)+", "+strings.TrimSpace(state), ", "))
	}

	for _, former := range document.FormerNames {
		if name := strings.TrimSpace(former.Name); name != "" {
			submission.FormerNames = append(submission.FormerNames, name)
		}
	}

	return submission, nil
}

// PadCIK renders a CIK in the ten-digit form the submissions API and Wikidata
// both use.
func PadCIK(cik int) string {
	return fmt.Sprintf("%010d", cik)
}

// NormalizeCIK accepts a CIK in any of the forms it appears in the wild —
// "320193", "0000320193", "CIK0000320193" — and returns the ten-digit form.
//
// It exists because the three forms are all "the CIK" to a human and none of
// them are interchangeable to a URL, and because a hand-edited manual.tsv row is
// the most likely place for the wrong one to appear.
func NormalizeCIK(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "CIK"), "cik")
	trimmed = strings.TrimLeft(trimmed, "0")

	if trimmed == "" {
		return "", fmt.Errorf("invalid CIK %q: no digits", raw)
	}

	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid CIK %q: %w", raw, err)
	}

	return PadCIK(value), nil
}

// flexInt decodes a JSON value that EDGAR publishes as a number in one file and
// as a string in another.
type flexInt int64

// UnmarshalJSON implements [encoding/json.Unmarshaler].
func (f *flexInt) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		return nil
	}

	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("decoding EDGAR integer %s: %w", data, err)
	}

	*f = flexInt(value)

	return nil
}

// flexString decodes a JSON value that arrives as either a string or a number.
// EDGAR publishes the SIC code as a string in submissions and as a number
// elsewhere, and a decoder that insists on one of the two breaks on the day the
// other appears.
type flexString string

// UnmarshalJSON implements [encoding/json.Unmarshaler].
func (f *flexString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*f = flexString(text)

		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("decoding EDGAR value %s: %w", data, err)
	}

	*f = flexString(number.String())

	return nil
}
