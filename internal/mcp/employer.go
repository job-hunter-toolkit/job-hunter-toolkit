package mcp

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
)

// The bounds on lookup_employer. Nothing here touches the network, so the only
// cost being bounded is the size of the reply.
const (
	defaultEmployerLimit = 50
	maxEmployerLimit     = 500
)

// employerArgs is the argument set of lookup_employer.
type employerArgs struct {
	Companies []string `json:"companies"`
	Limit     int      `json:"limit"`
}

// employerEntry pairs a covered job board with what is known about the employer
// behind it.
type employerEntry struct {
	// Source is the job board this row describes. It is always present, which is
	// what lets an unresolved company be reported as a fact rather than as a
	// gap in the list.
	Source Source `json:"source"`

	// Known reports whether a reviewed row exists. When false, Employer is nil
	// and that means nobody has resolved this company yet — not that it is
	// private, and not that it does not exist.
	Known bool `json:"known"`

	// Employer is the reviewed row, when there is one. It carries its own match
	// provenance: which rule tied this board to an external entity, how far that
	// tie can be trusted, and when the facts were retrieved.
	Employer *enrich.Employer `json:"employer,omitempty"`
}

// employerLookupResult is the structured payload of lookup_employer.
type employerLookupResult struct {
	Employers []employerEntry `json:"employers"`

	// Matched is how many job boards the terms selected, Known how many of those
	// have a reviewed row, and Returned how many are in this reply.
	Matched  int `json:"matched"`
	Known    int `json:"known"`
	Returned int `json:"returned"`

	// TableRows is the size of the whole reviewed table, so a caller reading
	// "known: 0" can see whether that means "unresolved" or "no table loaded".
	TableRows int `json:"table_rows"`
}

// lookupEmployer reports what the reviewed table knows about the employers
// behind the matching job boards. It makes no requests.
func (s *Server) lookupEmployer(raw json.RawMessage) (any, *rpcError) {
	var args employerArgs

	if rpcErr := decodeArgs(raw, &args); rpcErr != nil {
		return nil, rpcErr
	}

	terms := usableTerms(args.Companies)
	if len(terms) == 0 {
		return refuse("lookup_employer requires a non-empty \"companies\" argument.\n\n" +
			"Call list_companies to find registered names, or list_platforms for coverage totals.")
	}

	if s.Employers == nil {
		return refuse("No employer table is loaded, so nothing is known about any employer in this session.\n\n" +
			"This is a server configuration problem, not a fact about the companies you asked about. " +
			"search_jobs, list_companies and list_platforms are unaffected.")
	}

	sources := s.Catalog.Select(terms)
	if len(sources) == 0 {
		return refuse("No job board matches %s.\n\n"+
			"Terms are matched as case-insensitive substrings of the company name and the ATS slug. "+
			"Call list_companies with a shorter or differently spelled term to find the registered name.",
			quoteTerms(terms))
	}

	// Sorted before truncation so that a limited reply is the same first page
	// every time, rather than whichever boards the registry happened to list
	// first.
	slices.SortFunc(sources, compareSources)

	limit := defaultEmployerLimit
	if args.Limit > 0 {
		limit = min(args.Limit, maxEmployerLimit)
	}

	result := employerLookupResult{
		Matched:   len(sources),
		TableRows: s.Employers.Len(),
	}

	entries := make([]employerEntry, 0, min(len(sources), limit))

	for _, source := range sources {
		employer, known := s.Employers.For(source.Posting())
		if known {
			result.Known++
		}

		if len(entries) < limit {
			entries = append(entries, employerEntry{
				Source:   source,
				Known:    known,
				Employer: employer,
			})
		}
	}

	result.Employers = entries
	result.Returned = len(entries)

	return succeed(result)
}

// compareSources orders job boards by company, then platform, then key, so a
// reply is stable across runs.
func compareSources(a, b Source) int {
	if c := strings.Compare(strings.ToLower(a.Company), strings.ToLower(b.Company)); c != 0 {
		return c
	}

	if c := strings.Compare(a.Platform, b.Platform); c != 0 {
		return c
	}

	return strings.Compare(a.Key, b.Key)
}
