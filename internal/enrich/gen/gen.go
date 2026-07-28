// Package gen builds the committed enrichment tables.
//
// It runs offline from the CLI's point of view: in GitHub Actions, on a monthly
// schedule, in a workflow of its own that is deliberately not the nightly crawl.
// Its output is a diff — internal/enrich/data/employers.tsv and a candidates
// file — that a human reads before it becomes data anybody trusts.
//
// The order of work is the order of safety:
//
//  1. Fetch one seed file listing every SEC filer. One request, not 2,131.
//  2. Fetch, in one query, every official website Wikidata publishes against a
//     CIK. This is corroboration rather than decoration, so it has to arrive
//     before the matching decision rather than after it.
//  3. Resolve crawled sources to filers offline, accepting only matches that
//     are unique in both directions *and* rest on a name long enough to
//     identify somebody, and writing everything else to the candidates file
//     for review.
//  4. Fetch registration facts for the accepted matches only, paced under
//     EDGAR's published 10 requests per second.
//  5. Decorate those rows from Wikidata by CIK, never by name.
//
// Nothing in this package is reachable from the CLI's import graph.
package gen

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/edgar"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/resolve"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/wikidata"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// The files a run writes. manual.tsv and wages.tsv are deliberately absent: the
// first belongs to humans and the second has no generator yet, and a run that
// truncated either would destroy the only records this feature keeps.
const (
	// EmployersFile is the generated table the binary embeds.
	EmployersFile = "employers.tsv"

	// CandidatesFile is the review queue. It is never loaded by the CLI; it
	// exists so a person can see what was rejected and why, and promote the
	// rows they can confirm into manual.tsv by hand.
	CandidatesFile = "candidates.tsv"

	// BlankCheckSIC is EDGAR's industry code for a shell company formed to
	// hold a listing until it acquires an operating business.
	BlankCheckSIC = "6770"
)

// Options configures a generator run.
type Options struct {
	// Sources are the crawled boards to resolve. Empty means every builtin
	// source, which is what a real run wants; tests pass a handful.
	Sources []resolve.Source

	// EDGAR and Wikidata are the paced, contact-identified clients for each
	// upstream, built by fetch.Client. A nil Wikidata client skips the
	// decoration step, which is how a run can refresh EDGAR alone. Tests supply
	// a client over a fixture transport, so nothing here needs a live listener.
	EDGAR    *http.Client
	Wikidata *http.Client

	// Now is the retrieval date recorded on every row. Injected so a test can
	// assert an exact table rather than today's date.
	Now time.Time

	// Logger receives progress. Diagnostics go to stderr, per the roadmap; the
	// tables go to files rather than stdout because they are reviewed as a diff.
	Logger *slog.Logger
}

// Result is one run's output.
type Result struct {
	// Employers are the rows to commit.
	Employers []*enrich.Employer

	// Candidates are the matches a human must decide about.
	Candidates []resolve.Candidate

	// Stats is what the run should report in a pull request description, with
	// coverage first: this table will be mostly empty for a long time, and a
	// reader who is not told that up front will read it as breakage.
	Stats Stats
}

// Stats counts one run.
type Stats struct {
	Sources          int
	Filers           int
	FilerWebsites    int
	Matched          int
	Shells           int
	Candidates       int
	SubmissionsOK    int
	SubmissionsFail  int
	WikidataMatched  int
	WikidataAttempts int
}

// String renders the stats as one line for a log or a workflow summary.
func (s Stats) String() string {
	return fmt.Sprintf("%d/%d sources matched (%d filers seen, %d with a corroborating website, "+
		"%d shells refused, %d candidates for review), "+
		"%d submissions fetched, %d failed, %d decorated from Wikidata of %d asked",
		s.Matched, s.Sources, s.Filers, s.FilerWebsites, s.Shells, s.Candidates,
		s.SubmissionsOK, s.SubmissionsFail, s.WikidataMatched, s.WikidataAttempts)
}

// SourcesFrom converts the crawler's registry into the resolver's view.
func SourcesFrom(sources []services.Source) []resolve.Source {
	converted := make([]resolve.Source, 0, len(sources))

	for _, source := range sources {
		converted = append(converted, resolve.Source{
			Platform: source.Platform,
			Key:      source.Key,
			Company:  source.Company,
		})
	}

	return converted
}

// Run performs a generator pass.
func Run(ctx context.Context, opts Options) (*Result, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	if opts.EDGAR == nil {
		return nil, fmt.Errorf("generating enrichment tables: no EDGAR client; build one with fetch.Client so requests carry a contact and stay under 10 per second")
	}

	sources := opts.Sources
	if len(sources) == 0 {
		sources = SourcesFrom(services.Builtin)
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	retrieved := now.UTC().Format(time.DateOnly)

	filers, err := edgar.CompanyTickers(ctx, opts.EDGAR)
	if err != nil {
		return nil, err
	}

	// Before matching, not after: a short name is only committed when an
	// identifier the filer owns agrees with it, and the resolver cannot ask
	// that question with an empty websites map. A nil Wikidata client is
	// therefore not merely "skip the decoration" any more — it also means every
	// short-name match goes to the review queue, which is the safe direction.
	websites := map[string][]string{}

	if opts.Wikidata != nil {
		if websites, err = wikidata.Websites(ctx, opts.Wikidata); err != nil {
			return nil, err
		}
	}

	entities := make([]resolve.Entity, 0, len(filers))
	for _, filer := range filers {
		entities = append(entities, resolve.Entity{
			ID:       filer.CIK,
			Name:     filer.Name,
			Ticker:   filer.Ticker,
			Websites: websites[filer.CIK],
		})
	}

	resolved := resolve.Sources(sources, entities)

	result := &Result{
		Candidates: resolved.Candidates,
		Stats: Stats{
			Sources:       len(sources),
			Filers:        len(filers),
			FilerWebsites: len(websites),
			Matched:       len(resolved.Matches),
			Candidates:    len(resolved.Candidates),
		},
	}

	logger.InfoContext(ctx, "resolved crawled sources to SEC filers",
		slog.Int("sources", len(sources)),
		slog.Int("filers", len(filers)),
		slog.Int("matched", len(resolved.Matches)),
		slog.Int("candidates", len(resolved.Candidates)),
	)

	for _, match := range resolved.Matches {
		employer := &enrich.Employer{
			Source:    internal.PostingSource{Platform: match.Source.Platform, Key: match.Source.Key},
			Company:   match.Source.Company,
			LegalName: match.Entity.Name,
			CIK:       match.Entity.ID,
			Ticker:    match.Entity.Ticker,
			// Every entity in company_tickers.json trades under a ticker, so
			// this is a derived fact rather than an assumption. It is the one
			// place in this table where a false is as meaningful as a true, and
			// only a source that reaches this line ever gets one.
			Public: boolPtr(true),
			Match: enrich.Match{
				Method:      match.Method,
				Confidence:  match.Confidence,
				DataSources: []string{"sec-edgar"},
				RetrievedAt: retrieved,
			},
		}

		submission, err := edgar.Submissions(ctx, opts.EDGAR, match.Entity.ID)
		if err != nil {
			// Kept rather than dropped: the CIK, ticker and legal name from the
			// seed file are already useful, and the missing columns are visibly
			// empty rather than wrong. The run-level check below is what stops a
			// systematic failure from being committed as "no industry known".
			result.Stats.SubmissionsFail++

			logger.WarnContext(ctx, "SEC submissions fetch failed",
				slog.String("cik", match.Entity.ID),
				slog.String("company", match.Source.Company),
				slog.String("cause", err.Error()),
			)
		} else {
			result.Stats.SubmissionsOK++

			// A blank-check company is a registered shell with no operations
			// and no staff, filed to hold a listing until a merger fills it.
			// Whatever board carries this name, it is not that entity's, and
			// the name is the only reason the two were joined. Measured on the
			// 2026-07-28 run: 1 of 263 matches carried SIC 6770, personio's
			// "dynamix" matched to a SPAC, and no correct match carried it.
			if submission.SIC == BlankCheckSIC {
				result.Stats.Shells++

				result.Candidates = append(result.Candidates, resolve.Candidate{
					Source:     match.Source,
					Entity:     match.Entity,
					Method:     match.Method,
					Confidence: enrich.ConfidenceMedium,
					Why: "blank-check shell: " + submission.Name + " files under SIC " + BlankCheckSIC +
						", which is a listing waiting for a merger rather than an employer with a job board",
				})

				continue
			}

			employer.LegalName = cmp.Or(submission.Name, employer.LegalName)
			employer.SIC = submission.SIC
			employer.Industry = submission.SICDescription
			employer.Headquarters = submission.Business

			if len(submission.Exchanges) > 0 {
				employer.Exchange = submission.Exchanges[0]
			}

			if employer.Ticker == "" && len(submission.Tickers) > 0 {
				employer.Ticker = submission.Tickers[0]
			}
		}

		result.Employers = append(result.Employers, employer)
	}

	// Counted from what survived rather than from what was proposed: the shell
	// check above runs after the fetch, so the numbers a reviewer reads at the
	// top of the table have to be taken after it.
	result.Stats.Matched = len(result.Employers)
	result.Stats.Candidates = len(result.Candidates)

	// A run where most submission fetches failed is a blocked run, and its table
	// would be a full set of rows with every industry blank. Committing that
	// would look like data and read like an answer, so it is refused here rather
	// than left for a reviewer to notice.
	if result.Stats.SubmissionsFail > 0 && result.Stats.SubmissionsFail*5 >= result.Stats.Matched {
		return nil, fmt.Errorf("refusing to write enrichment tables: %d of %d SEC submission fetches failed, which looks like a block or an endpoint change rather than a few missing filers",
			result.Stats.SubmissionsFail, result.Stats.Matched)
	}

	if err := decorate(ctx, opts.Wikidata, result, logger); err != nil {
		return nil, err
	}

	return result, nil
}

// decorate adds the Wikidata facts EDGAR does not publish, joined by CIK.
func decorate(ctx context.Context, client *http.Client, result *Result, logger *slog.Logger) error {
	if client == nil {
		return nil
	}

	ciks := make([]string, 0, len(result.Employers))
	for _, employer := range result.Employers {
		if employer.CIK != "" {
			ciks = append(ciks, employer.CIK)
		}
	}

	if len(ciks) == 0 {
		return nil
	}

	result.Stats.WikidataAttempts = len(ciks)

	companies, err := wikidata.ByCIK(ctx, client, ciks)
	if err != nil {
		return err
	}

	for _, employer := range result.Employers {
		company, ok := companies[employer.CIK]
		if !ok {
			continue
		}

		result.Stats.WikidataMatched++

		employer.WikidataID = company.QID
		employer.Employees = company.Employees
		employer.Founded = company.Founded
		employer.Parent = company.Parent

		// EDGAR wins where both have an answer: its industry is a code from a
		// filing the company signed, and its address is the one it registered.
		// Wikidata fills the gaps rather than overwriting.
		employer.Industry = cmp.Or(employer.Industry, company.Industry)
		employer.Headquarters = cmp.Or(employer.Headquarters, company.Headquarters)

		employer.Match.DataSources = append(employer.Match.DataSources, "wikidata")
	}

	logger.InfoContext(ctx, "decorated employers from Wikidata",
		slog.Int("asked", result.Stats.WikidataAttempts),
		slog.Int("matched", result.Stats.WikidataMatched),
	)

	return nil
}

// WriteTables writes the generated table and the review queue into dir.
func (r *Result) WriteTables(dir string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	notes := []string{
		"employers.tsv -- GENERATED. Do not hand-edit; use manual.tsv instead.",
		"",
		"Regenerate with:",
		"  go run ./tools/enrichgen -out internal/enrich/data -contact you@example.com",
		"",
		fmt.Sprintf("Generated %s from SEC EDGAR (public domain) and Wikidata (CC0).", now.UTC().Format(time.DateOnly)),
		fmt.Sprintf("Coverage: %s.", r.Stats),
		"",
		"Only matches that are unique in both directions are written here. Everything",
		"else is in candidates.tsv for a human to confirm and promote into manual.tsv.",
		"An unmatched company is a correct answer, not a gap to fill by guessing.",
	}

	path := filepath.Join(dir, EmployersFile)

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}

	if err := enrich.WriteEmployers(file, r.Employers, notes...); err != nil {
		file.Close()

		return err
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}

	return r.writeCandidates(filepath.Join(dir, CandidatesFile), now)
}

// candidateColumns is the review queue's schema. It is not loaded by anything,
// so it is free to be shaped for a reader rather than for a parser: the reason
// comes last because it is the long field and the one being read.
var candidateColumns = []string{
	"platform", "key", "company", "cik", "entity_name", "ticker",
	"method", "confidence", "why",
}

// writeCandidates writes the review queue.
func (r *Result) writeCandidates(path string, now time.Time) error {
	rows := slices.Clone(r.Candidates)

	// Sorted so a reviewer diffing two runs sees what changed rather than what
	// moved.
	slices.SortFunc(rows, func(a, b resolve.Candidate) int {
		return strings.Compare(
			a.Source.Platform+"\x00"+a.Source.Key+"\x00"+a.Entity.ID,
			b.Source.Platform+"\x00"+b.Source.Key+"\x00"+b.Entity.ID,
		)
	})

	var b strings.Builder

	fmt.Fprintf(&b, "# candidates.tsv -- REVIEW QUEUE, generated %s. Nothing here is used by the binary.\n",
		now.UTC().Format(time.DateOnly))
	b.WriteString("#\n")
	b.WriteString("# Each row is a match the generator considered and refused, with the reason.\n")
	b.WriteString("# To accept one: confirm it by hand, then add a row for it to manual.tsv with\n")
	b.WriteString("# match_method=manual and match_confidence=high. Do not paste rows into\n")
	b.WriteString("# employers.tsv; the next run overwrites that file.\n")
	b.WriteString(strings.Join(candidateColumns, "\t"))
	b.WriteByte('\n')

	for _, candidate := range rows {
		fields := []string{
			candidate.Source.Platform,
			candidate.Source.Key,
			candidate.Source.Company,
			candidate.Entity.ID,
			candidate.Entity.Name,
			candidate.Entity.Ticker,
			string(candidate.Method),
			string(candidate.Confidence),
			candidate.Why,
		}

		for i, field := range fields {
			// A tab inside a value would shift every later column, and the row
			// would still look parseable. Upstream names are free text from
			// third parties, so this is sanitised rather than trusted.
			fields[i] = strings.Join(strings.Fields(field), " ")
		}

		b.WriteString(strings.Join(fields, "\t"))
		b.WriteByte('\n')
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// boolPtr returns a pointer to v, for the tri-state fields.
func boolPtr(v bool) *bool { return &v }
