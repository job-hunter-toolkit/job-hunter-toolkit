// Package enrich attaches what is known about an employer to the postings that
// employer published, so the toolkit supports market research and qualification
// matching rather than only listing openings.
//
// # Nothing here makes a request
//
// Every fact this package serves comes out of a table compiled into the binary
// by go:embed. That is not a performance preference, it is the only shape this
// project can afford. The crawl measured on 2026-07-26 recorded
// "07/26/26 473404 1772 partial": 473,404 postings from 1,772 companies, and it
// did not finish inside 350 minutes on GitHub Actions. Adding one third-party
// lookup per company to that path would add ~2,131 requests to a run that
// already misses its budget, and SEC EDGAR alone caps every client at 10
// requests per second across all of its hosts, so those lookups cannot even be
// parallelised out of the way. A lookup here is one map read.
//
// It also keeps the promise docs/architecture-roadmap.md makes about the
// default binary: no CGO, no required state, no daemon, and no network
// dependency the crawl does not already have.
//
// # The table is a reviewed artifact, not a runtime guess
//
// The crawler's identity for a company is an ATS slug ("paloaltonetworks2"), a
// Workday tenant URL, or a Phenom hostname. Every external source keys on
// something else: a legal name, a CIK, an all-caps DOL employer string. Closing
// that gap by fuzzy-matching at query time would sooner or later attribute one
// company's headcount or industry to another, and a plausible wrong number is
// indistinguishable from a right one at 473,404 postings of scale. It is the
// same failure docs/compensation.md was written to prevent for pay.
//
// So the match is made once, offline, by the generator in
// internal/enrich/gen; only unambiguous matches are written to the table; the
// rest land in a candidates file for a human to promote by hand; and every
// committed row carries the method and confidence that produced it, so a bad
// row is auditable rather than invisible. Unmatched is a perfectly good answer
// and is the default for any company nobody has resolved yet.
//
// # The join key
//
// Rows are keyed by [internal.PostingSource]: the platform plus the tenant key
// that docs/architecture-roadmap.md settles on as the stable integration ID.
// Never by the company display name. Phenom and Workday derive that name from a
// hostname or tenant URL, services.Companies deduplicates it case-insensitively,
// and one Workday tenant can host several brands, so distinct employers collide
// on the display string. A company that moves from Greenhouse to Ashby gets a
// new row rather than silently inheriting the old employer's facts.
package enrich

import (
	"iter"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// Employer is what is known about the company behind one crawled source.
//
// Everything except Source and Match is omitted when unknown, following the
// rule [internal.JobPosting] already sets for its enrichment fields: absence
// means "nobody has established this", never "no". A consumer that sees no
// `employees` key learns that the number was never resolved, which is a
// different and more useful statement than a zero.
type Employer struct {
	// Source is the integration this row describes: the platform and tenant key
	// the crawler fetched with. It is the join key, and it is what makes a row
	// correctable — a reviewer can see exactly which board slug a legal name was
	// attached to.
	Source internal.PostingSource `json:"source"`

	// Company is the display name the source was registered under, carried in
	// the table purely so a human reviewing a diff can read it. Nothing joins on
	// it; see the package comment.
	Company string `json:"company,omitempty"`

	// LegalName is the registered entity name as the authoritative source spells
	// it ("Palo Alto Networks, Inc."), which is rarely how the board slug spells
	// it.
	LegalName string `json:"legal_name,omitempty"`

	// CIK is the SEC's Central Index Key, zero-padded to ten digits. It is the
	// strongest identifier here: Wikidata publishes it as P5531, so a row
	// matched on CIK is joined by identifier rather than by name.
	CIK string `json:"cik,omitempty"`

	// Ticker and Exchange are the primary listing, when there is one.
	Ticker   string `json:"ticker,omitempty"`
	Exchange string `json:"exchange,omitempty"`

	// SIC is EDGAR's four-digit Standard Industrial Classification code and
	// Industry its description ("7372 Prepackaged Software"), or the Wikidata
	// industry label for employers EDGAR does not cover.
	SIC      string `json:"sic,omitempty"`
	Industry string `json:"industry,omitempty"`

	// Public reports whether the employer files with the SEC.
	//
	// A pointer, for the same reason [internal.JobPosting.Remote] is one: with a
	// plain bool, "privately held" and "nobody checked" would both be false, and
	// most companies this project crawls are startups nobody has checked. Nil
	// means unknown. The absence of an EDGAR filing is not evidence of being
	// private — it is evidence of not having been found.
	Public *bool `json:"public,omitempty"`

	// Employees is the headcount the source published, and AsOf on Match says
	// when it was retrieved. It is a snapshot of a number that moves, so treat
	// it as an order of magnitude rather than a fact about today.
	Employees int `json:"employees,omitempty"`

	// Founded is the year the entity was founded.
	Founded int `json:"founded,omitempty"`

	// Headquarters is the primary location, as the source labels it.
	Headquarters string `json:"headquarters,omitempty"`

	// Parent is the parent organization, which is the field that explains why a
	// posting from an unfamiliar brand is worth reading.
	Parent string `json:"parent,omitempty"`

	// WikidataID is the Q-identifier of the matching Wikidata item.
	WikidataID string `json:"wikidata_id,omitempty"`

	// Match records how this row was resolved and how much to trust it.
	Match Match `json:"match"`

	// WageBenchmarks are third-party pay distributions for this employer. They
	// are NOT this employer's published pay; see [WageBenchmark].
	WageBenchmarks []WageBenchmark `json:"wage_benchmarks,omitempty"`
}

// Match is the provenance of one table row: how the crawled source was tied to
// an external entity, and how far that tie can be trusted.
//
// It is stored per row rather than assumed per table because the two matching
// methods are not equally safe. A row joined by CIK is joined by identifier; a
// row joined because two normalized names happened to be equal is joined by
// coincidence that held once. Recording which is which is what makes a wrong
// row findable later instead of permanent.
type Match struct {
	// Method names the rule that produced the row, such as "edgar-exact-name"
	// or "manual".
	Method Method `json:"method,omitempty"`

	// Confidence is the reviewer-facing grade. Only [ConfidenceHigh] is written
	// to the table by the generator; anything weaker goes to the candidates file
	// for a human to promote.
	Confidence Confidence `json:"confidence,omitempty"`

	// DataSources names the upstream datasets the facts came from, such as
	// "sec-edgar" or "wikidata", so an attribution requirement can be honoured
	// and a bad upstream can be traced.
	DataSources []string `json:"data_sources,omitempty"`

	// RetrievedAt is the date (YYYY-MM-DD) the facts were fetched. Headcount and
	// industry age at different speeds and a consumer deserves to know which
	// year it is looking at.
	RetrievedAt string `json:"retrieved_at,omitempty"`
}

// Method names the rule that resolved a crawled source to an external entity.
type Method string

// The resolution methods.
//
// The set is small and closed on purpose: every additional rule is another way
// to be confidently wrong, so a new one has to be added here, tested, and shown
// in the table before it can attach a single fact.
const (
	// MethodUnknown is the zero value, used by hand-built tables in tests.
	MethodUnknown Method = ""

	// MethodManual means a human wrote the row. It is the only method that can
	// enter the table without the generator agreeing.
	MethodManual Method = "manual"

	// MethodEDGARExactName means the source's display name and an EDGAR filer's
	// name normalized to the same string, and that match was unique in both
	// directions: exactly one source and exactly one filer. Ambiguity is not
	// resolved by picking the first, it is refused.
	MethodEDGARExactName Method = "edgar-exact-name"

	// MethodEDGARExactKey is [MethodEDGARExactName] against the ATS tenant key
	// rather than the display name, which is what catches boards whose slug is
	// the legal name run together ("paloaltonetworks").
	MethodEDGARExactKey Method = "edgar-exact-key"

	// MethodWikidataCIK means a Wikidata item was joined to an already-matched
	// row by CIK (property P5531). It resolves nothing by itself; it only
	// decorates a row EDGAR already identified, which is why it is safe.
	MethodWikidataCIK Method = "wikidata-cik"
)

// Confidence grades a match for a human reviewer.
type Confidence string

// The confidence grades.
const (
	// ConfidenceUnknown is the zero value.
	ConfidenceUnknown Confidence = ""

	// ConfidenceHigh means an identifier match, or a normalized-name match that
	// was unique on both sides. Only these are committed automatically.
	ConfidenceHigh Confidence = "high"

	// ConfidenceMedium means a plausible match that something else could also
	// explain: an ambiguous name, or a name that matched only after aggressive
	// normalization. These go to the candidates file, never to the table.
	ConfidenceMedium Confidence = "medium"

	// ConfidenceLow means a weak signal recorded so a reviewer can see it was
	// considered and rejected.
	ConfidenceLow Confidence = "low"
)

// WageSource names the dataset a [WageBenchmark] was aggregated from.
type WageSource string

// The wage benchmark sources.
const (
	// WageSourceOFLC is the US Department of Labor's OFLC disclosure data: what
	// an employer certified it would pay on a specific H-1B or PERM application.
	WageSourceOFLC WageSource = "oflc"

	// WageSourceOEWS is the BLS Occupational Employment and Wage Statistics
	// survey: an estimate for an occupation in an area, not for an employer.
	WageSourceOEWS WageSource = "oews"
)

// WageBenchmark is a third-party pay distribution for an occupation, in USD per
// year.
//
// It is deliberately not, and must never become, [internal.Compensation].
// docs/compensation.md states that nothing blends sources, and
// [internal.JobPosting.Compensation] is documented as the range the employer
// published with that posting. A DOL benchmark is a statutory wage an employer
// certified on somebody else's visa application, and a BLS benchmark is a survey
// estimate about an occupation in a metro area. Neither is what this employer
// pays for this job. Writing either into Compensation, or letting either satisfy
// --min-pay, would poison the one field this project is careful about, so they
// live here, in a separate object, under a separate name, behind separate flags.
type WageBenchmark struct {
	// SOC is the six-digit Standard Occupational Classification code the
	// distribution is for, and Occupation its title.
	SOC        string `json:"soc,omitempty"`
	Occupation string `json:"occupation,omitempty"`

	// Area is the geography: "US" for national, a two-letter state code, or a
	// metro name. Pay for one occupation varies more by geography than by
	// almost anything else, so a benchmark without an area is not a benchmark.
	Area string `json:"area,omitempty"`

	// Source names the dataset. Two rows from different datasets are not
	// comparable and must not be averaged together.
	Source WageSource `json:"source,omitempty"`

	// N is how many underlying records the distribution was computed from. A
	// percentile over three certifications is not a market rate, and a consumer
	// cannot tell without this number.
	N int `json:"n,omitempty"`

	// P25, P50 and P75 are the quartiles, annualized to USD per year so that
	// hourly and salaried certifications are comparable, using the same
	// working-time assumptions as [internal.Compensation].
	P25 float64 `json:"p25,omitempty"`
	P50 float64 `json:"p50,omitempty"`
	P75 float64 `json:"p75,omitempty"`

	// AsOf is the source period the rows came from, such as "FY2025" for an
	// OFLC fiscal year or "2024-05" for an OEWS reference month.
	AsOf string `json:"as_of,omitempty"`
}

// Posting is a job posting with what is known about its employer attached.
//
// [internal.JobPosting] is embedded rather than copied, so this marshals to
// exactly the JSON that posting already produced plus one additive
// `employer` key, and only when there is an employer to report. Existing
// NDJSON consumers reading .company or .url are unaffected;
// TestPostingJSONIsAdditive asserts the byte-for-byte equality rather than
// trusting the reading.
//
// The nesting is deliberate. Flattening an employer's facts to the top level
// would put a third party's claim about a company in the same namespace as the
// board's claim about a job, which is the trust boundary docs/compensation.md
// draws for pay provenance and the same one applies here.
type Posting struct {
	*internal.JobPosting

	// Employer is nil for every posting whose source has no reviewed row, which
	// is the common case and not an error.
	Employer *Employer `json:"employer,omitempty"`
}

// Postings is a sequence of enriched postings, mirroring [internal.Jobs] so the
// two compose as stream decorators.
type Postings = iter.Seq2[*Posting, error]

// Attach returns jobs with each posting's employer facts attached, looked up by
// the posting's [internal.PostingSource].
//
// It is a stream decorator in the shape of [internal.Filter.Apply] and
// [internal.Dedupe]: lazy, single-pass, and error-transparent. Errors pass
// through untouched, because a source that failed still has to be reported as
// failed — docs/architecture-roadmap.md requires that a failed source cannot
// make previously seen jobs look removed, and swallowing its error here would
// do exactly that one layer up.
//
// Postings whose source identity is empty are passed through with no employer.
// Falling back to the company display name would be the runtime fuzzy match the
// package comment refuses; a posting from an adapter that has not been migrated
// gets nothing rather than getting somebody else's data.
func (t *Table) Attach(jobs internal.Jobs) Postings {
	return func(yield func(*Posting, error) bool) {
		for job, err := range jobs {
			if err != nil {
				if !yield(nil, err) {
					return
				}

				continue
			}

			if job == nil {
				continue
			}

			// One map read per posting, no allocation beyond the wrapper: at the
			// measured 473,404 postings this is microseconds of the crawl, which
			// is the entire point of embedding the table.
			employer, _ := t.For(job.Source)

			if !yield(&Posting{JobPosting: job, Employer: employer}, nil) {
				return
			}
		}
	}
}

// Flatten returns the underlying postings, dropping the employer facts.
//
// It exists so an enrichment filter can be spliced into a pipeline whose
// consumer still expects [internal.Jobs] — the CSV and NDJSON writers in
// package main are that consumer — without those writers having to learn a new
// type. Filtering on enrichment and printing the unchanged posting shape is a
// legitimate combination, and the frozen CSV column set makes it the default
// one.
func Flatten(postings Postings) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		for posting, err := range postings {
			if err != nil {
				if !yield(nil, err) {
					return
				}

				continue
			}

			if posting == nil {
				continue
			}

			if !yield(posting.JobPosting, nil) {
				return
			}
		}
	}
}
