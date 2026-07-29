package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/query"
)

// searchArgs is the argument set of search_jobs. It mirrors [query.Query] plus
// the two arguments that are about this server rather than about postings:
// companies, which selects what gets fetched, and limit, which bounds the reply.
type searchArgs struct {
	Companies []string `json:"companies"`

	Titles           []string `json:"titles"`
	ExcludeTitles    []string `json:"exclude_titles"`
	Locations        []string `json:"locations"`
	Departments      []string `json:"departments"`
	Remote           bool     `json:"remote"`
	HasCompensation  bool     `json:"has_compensation"`
	MinAnnual        float64  `json:"min_annual"`
	EmploymentTypes  []string `json:"employment_types"`
	WorkplaceTypes   []string `json:"workplace_types"`
	PostedSince      string   `json:"posted_since"`
	PostedWithinDays int      `json:"posted_within_days"`

	Limit int `json:"limit"`
}

// searchResult is the structured payload of a successful search.
type searchResult struct {
	// Postings are the matches, sorted deterministically and truncated to the
	// requested limit.
	Postings []*jobposting.JobPosting `json:"postings"`

	// Summary reports what the crawl actually did, which the postings alone
	// cannot say. An empty list from a complete crawl and an empty list from a
	// crawl that timed out mean opposite things.
	Summary searchSummary `json:"summary"`
}

// searchSummary describes the crawl behind a result.
type searchSummary struct {
	// Complete reports whether every selected source was fetched to exhaustion.
	// False means the answer is a floor, not a total.
	Complete bool `json:"complete"`

	// IncompleteReason names why, when Complete is false.
	IncompleteReason string `json:"incomplete_reason,omitempty"`

	// SourcesSelected is how many job boards the company terms chose, and
	// SourcesFailed how many yielded an error instead of postings. A board that
	// has been retired is an ordinary event at this scale, not a bug.
	SourcesSelected int `json:"sources_selected"`
	SourcesFailed   int `json:"sources_failed"`

	// Companies lists the companies actually searched, so a caller can see that
	// "data" selected twenty-four boards it did not intend.
	Companies []string `json:"companies"`

	// Matched is how many postings satisfied the filters, Returned how many are
	// in this reply. They differ when Truncated is set.
	Matched   int  `json:"matched"`
	Returned  int  `json:"returned"`
	Truncated bool `json:"truncated"`

	// Errors is a small, sorted, deduplicated sample of source failures, kept
	// short because a caller needs to know that boards failed and roughly why,
	// not to read every failure.
	Errors []string `json:"errors,omitempty"`
}

// maxReportedErrors caps the error sample in a search summary.
const maxReportedErrors = 3

// searchJobs runs a bounded, source-narrowed crawl and returns matching
// postings.
func (s *Server) searchJobs(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args searchArgs

	if rpcErr := decodeArgs(raw, &args); rpcErr != nil {
		return nil, rpcErr
	}

	limits := s.Limits.withDefaults()

	// The bound, in order: a company term must exist, must select something, and
	// must not select more than one tool call can afford.
	terms := usableTerms(args.Companies)
	if len(terms) == 0 {
		return refuse("search_jobs requires a non-empty \"companies\" argument.\n\n" +
			"Postings are fetched from company job boards at call time, and naming companies is what selects which boards are fetched. " +
			"Without it this would crawl every board in the registry, which takes about 15 minutes — far longer than a tool call can run.\n\n" +
			"If you do not know which company to name, call list_companies to find candidates, then search those.")
	}

	sources := s.Catalog.Select(terms)
	if len(sources) == 0 {
		return refuse("No job board matches %s.\n\n"+
			"Terms are matched as case-insensitive substrings of the company name and the ATS slug. "+
			"Call list_companies with a shorter or differently spelled term to find the registered name.",
			quoteTerms(terms))
	}

	if len(sources) > limits.MaxSources {
		return refuse("%s matches %d job boards, more than the %d this tool will crawl in one call.\n\n"+
			"Crawling that many takes longer than a tool call can run, so nothing was fetched. "+
			"Use a more specific company term, or split the search across several calls. "+
			"Call list_companies with the same terms to see which companies matched.",
			quoteTerms(terms), len(sources), limits.MaxSources)
	}

	filter, rpcErr := args.filter()
	if rpcErr != nil {
		return nil, rpcErr
	}

	limit := limits.DefaultLimit
	if args.Limit > 0 {
		limit = min(args.Limit, limits.MaxLimit)
	}

	crawlCtx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()

	started := time.Now()

	s.logger().InfoContext(crawlCtx, "mcp search starting",
		slog.Int("sources", len(sources)),
		slog.Any("companies", terms))

	postings, summary := s.collect(crawlCtx, sources, filter)

	// Sorting is what makes two identical searches over an unchanged board
	// return the same bytes. The crawler yields postings in whatever order they
	// are fetched, which depends on how many workers were free and how fast each
	// board answered.
	slices.SortFunc(postings, comparePostings)

	summary.SourcesSelected = len(sources)
	summary.Companies = companyNames(sources)
	summary.Matched = len(postings)

	if len(postings) > limit {
		postings = postings[:limit]
		summary.Truncated = true
	}

	summary.Returned = len(postings)

	s.logger().InfoContext(crawlCtx, "mcp search finished",
		slog.Int("matched", summary.Matched),
		slog.Int("returned", summary.Returned),
		slog.Int("sources_failed", summary.SourcesFailed),
		slog.Bool("complete", summary.Complete),
		slog.Duration("elapsed", time.Since(started)))

	// Postings must never be null in the payload: a client distinguishing "no
	// matches" from "field missing" should not have to.
	if postings == nil {
		postings = []*jobposting.JobPosting{}
	}

	return succeed(searchResult{Postings: postings, Summary: summary})
}

// collect drains the crawl, applying dedupe and the filter, and reports what
// happened. It never returns an error: a crawl that failed partway still has an
// honest partial answer to give, and saying so is the whole point of the
// summary.
func (s *Server) collect(ctx context.Context, sources []Source, filter query.Query) ([]*jobposting.JobPosting, searchSummary) {
	var (
		summary  searchSummary
		postings []*jobposting.JobPosting
		failures = make(map[string]struct{})
	)

	jobs := filter.Apply(jobposting.Dedupe(s.Catalog.Crawl(ctx, sources)))

	for posting, err := range jobs {
		if err != nil {
			// A dead board must not end the search: at this scale some fraction
			// of any selection has been retired since the registry was written.
			summary.SourcesFailed++
			failures[err.Error()] = struct{}{}

			continue
		}

		if posting != nil {
			postings = append(postings, posting)
		}
	}

	summary.Complete = true

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		summary.Complete = false
		summary.IncompleteReason = fmt.Sprintf(
			"the %s crawl budget expired before every job board answered; these postings are a partial result, not a complete one",
			s.Limits.withDefaults().Timeout)
	case ctx.Err() != nil:
		summary.Complete = false
		summary.IncompleteReason = "the search was cancelled before every job board answered; these postings are a partial result"
	}

	summary.Errors = sampleErrors(failures)

	return postings, summary
}

// sampleErrors reduces the failures seen during a crawl to a short, sorted,
// deduplicated sample. Sorted so the sample does not change between two
// otherwise identical runs.
func sampleErrors(failures map[string]struct{}) []string {
	if len(failures) == 0 {
		return nil
	}

	sample := make([]string, 0, len(failures))
	for message := range failures {
		sample = append(sample, message)
	}

	slices.Sort(sample)

	if len(sample) > maxReportedErrors {
		sample = sample[:maxReportedErrors]
	}

	return sample
}

// comparePostings orders postings so that identical inputs produce identical
// output. URL is the last key because it is the identity Dedupe uses, so it
// breaks every remaining tie.
func comparePostings(a, b *jobposting.JobPosting) int {
	return cmp.Or(
		strings.Compare(strings.ToLower(a.Company), strings.ToLower(b.Company)),
		strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
		strings.Compare(a.Location, b.Location),
		strings.Compare(a.URL, b.URL),
	)
}

// companyNames returns the distinct company names of the sources, sorted.
func companyNames(sources []Source) []string {
	names := make([]string, 0, len(sources))

	for _, source := range sources {
		names = append(names, source.Company)
	}

	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	return slices.CompactFunc(names, strings.EqualFold)
}

// filter converts the arguments into the shared filter vocabulary.
func (a searchArgs) filter() (query.Query, *rpcError) {
	postedSince, rpcErr := a.postedSince()
	if rpcErr != nil {
		return query.Query{}, rpcErr
	}

	employmentTypes, rpcErr := parseEmploymentTypes(a.EmploymentTypes)
	if rpcErr != nil {
		return query.Query{}, rpcErr
	}

	workplaceTypes, rpcErr := parseWorkplaceTypes(a.WorkplaceTypes)
	if rpcErr != nil {
		return query.Query{}, rpcErr
	}

	// Companies is deliberately not copied into the filter. It already narrowed
	// which boards were fetched, and applying it again as a posting filter would
	// drop postings whose company display name differs from the term that
	// selected the source — exactly the bug the CLI documents at main.go's
	// postingFilterFor.
	return query.Query{
		Titles:          a.Titles,
		ExcludeTitles:   a.ExcludeTitles,
		Locations:       a.Locations,
		Departments:     a.Departments,
		Remote:          a.Remote,
		HasCompensation: a.HasCompensation,
		MinAnnual:       a.MinAnnual,
		EmploymentTypes: employmentTypes,
		WorkplaceTypes:  workplaceTypes,
		PostedSince:     postedSince,
	}, nil
}

// postedSince resolves the two ways of asking for recency into one instant.
func (a searchArgs) postedSince() (time.Time, *rpcError) {
	if a.PostedSince != "" && a.PostedWithinDays > 0 {
		return time.Time{}, errorf(codeInvalidParams,
			"set either \"posted_since\" or \"posted_within_days\", not both")
	}

	if a.PostedWithinDays > 0 {
		return time.Now().UTC().AddDate(0, 0, -a.PostedWithinDays), nil
	}

	if a.PostedSince == "" {
		return time.Time{}, nil
	}

	// The date-only form first: it is what an agent writes, and parsing it as
	// RFC 3339 fails with a message about a missing timezone that reads as a bug.
	if when, err := time.Parse(time.DateOnly, a.PostedSince); err == nil {
		return when, nil
	}

	when, err := time.Parse(time.RFC3339, a.PostedSince)
	if err != nil {
		return time.Time{}, errorf(codeInvalidParams,
			"invalid \"posted_since\" value %q: want a date like 2026-01-31 or an RFC 3339 timestamp",
			a.PostedSince)
	}

	return when, nil
}

// parseEmploymentTypes maps the requested employment types onto the canonical
// vocabulary, rejecting values outside it.
//
// Rejecting rather than normalizing is deliberate here, even though
// [jobposting.NormalizeEmploymentType] would happily accept "Full-Time". The
// schema publishes a closed enum; silently accepting values outside it would
// let an agent learn a spelling that the next client validates and rejects.
func parseEmploymentTypes(values []string) ([]jobposting.EmploymentType, *rpcError) {
	var out []jobposting.EmploymentType

	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}

		typ := jobposting.EmploymentType(value)
		if !slices.Contains(jobposting.EmploymentTypeValues(), typ) {
			return nil, errorf(codeInvalidParams,
				"unknown employment_type %q; want one of %s",
				value, strings.Join(employmentTypeNames(), ", "))
		}

		out = append(out, typ)
	}

	return out, nil
}

// parseWorkplaceTypes maps the requested workplace types onto the canonical
// vocabulary, rejecting values outside it.
func parseWorkplaceTypes(values []string) ([]jobposting.WorkplaceType, *rpcError) {
	var out []jobposting.WorkplaceType

	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}

		typ := jobposting.WorkplaceType(value)
		if !slices.Contains(jobposting.WorkplaceTypeValues(), typ) {
			return nil, errorf(codeInvalidParams,
				"unknown workplace_type %q; want one of %s",
				value, strings.Join(workplaceTypeNames(), ", "))
		}

		out = append(out, typ)
	}

	return out, nil
}

// usableTerms drops blank entries, matching how the filter vocabulary treats
// them: a term of only spaces is a mistake, and answering a mistake with an
// empty result set reads as "nothing is hiring".
func usableTerms(terms []string) []string {
	out := make([]string, 0, len(terms))

	for _, term := range terms {
		if trimmed := strings.TrimSpace(term); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

// quoteTerms renders terms for an error message.
func quoteTerms(terms []string) string {
	quoted := make([]string, 0, len(terms))

	for _, term := range terms {
		quoted = append(quoted, strconv.Quote(term))
	}

	return strings.Join(quoted, ", ")
}
