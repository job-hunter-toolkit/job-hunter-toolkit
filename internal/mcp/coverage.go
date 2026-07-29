package mcp

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// The bounds on the two registry-reading tools. They are generous compared with
// the search budget because nothing here touches the network; the only cost
// being bounded is the size of the reply.
const (
	defaultCompanyLimit = 100
	maxCompanyLimit     = 1000
)

// companyArgs is the argument set of list_companies.
type companyArgs struct {
	Contains  []string `json:"contains"`
	Platforms []string `json:"platforms"`
	Limit     int      `json:"limit"`
	Offset    int      `json:"offset"`
}

// companyEntry is one company and the platforms it is registered on.
type companyEntry struct {
	Company string `json:"company"`

	// Platforms is every ATS this company has a board on, sorted. A company can
	// appear on more than one: a migration from Greenhouse to Ashby leaves both
	// registered until someone removes the old one.
	Platforms []string `json:"platforms"`

	// Keys are the tenant identifiers, sorted. They are shown because they are
	// also valid search terms, and because a Workday tenant URL is the only
	// unambiguous way to name some employers.
	Keys []string `json:"keys"`
}

// companyListResult is the structured payload of list_companies.
type companyListResult struct {
	Companies []companyEntry `json:"companies"`

	// Matched is how many companies satisfied the filters and Returned how many
	// are in this page, so a caller can tell a short page from the end of the
	// list.
	Matched  int `json:"matched"`
	Returned int `json:"returned"`
	Offset   int `json:"offset"`

	// TotalCompanies and TotalSources describe the whole registry, so a filtered
	// answer still carries the scale it was drawn from.
	TotalCompanies int `json:"total_companies"`
	TotalSources   int `json:"total_sources"`
}

// listCompanies reports which companies are covered. It makes no requests.
func (s *Server) listCompanies(raw json.RawMessage) (any, *rpcError) {
	var args companyArgs

	if rpcErr := decodeArgs(raw, &args); rpcErr != nil {
		return nil, rpcErr
	}

	all := s.Catalog.Sources()

	// Select rather than filtering here, so "contains" means exactly what the
	// same argument means to search_jobs. Two tools that spell matching
	// differently would make list_companies useless for finding search terms.
	selected := all
	if terms := usableTerms(args.Contains); len(terms) > 0 {
		selected = s.Catalog.Select(terms)
	}

	if platforms := usableTerms(args.Platforms); len(platforms) > 0 {
		selected = filterByPlatform(selected, platforms)
	}

	entries := groupByCompany(selected)

	limit := defaultCompanyLimit
	if args.Limit > 0 {
		limit = min(args.Limit, maxCompanyLimit)
	}

	result := companyListResult{
		Matched:        len(entries),
		Offset:         args.Offset,
		TotalCompanies: len(groupByCompany(all)),
		TotalSources:   len(all),
	}

	if args.Offset < len(entries) {
		entries = entries[args.Offset:]
	} else {
		entries = nil
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}

	if entries == nil {
		entries = []companyEntry{}
	}

	result.Companies = entries
	result.Returned = len(entries)

	return succeed(result)
}

// filterByPlatform keeps only sources on the named platforms, matched exactly
// rather than as substrings: platform names are a closed vocabulary the registry
// controls, so there is nothing to guess at.
func filterByPlatform(sources []Source, platforms []string) []Source {
	wanted := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		wanted[strings.ToLower(platform)] = struct{}{}
	}

	kept := make([]Source, 0, len(sources))

	for _, source := range sources {
		if _, ok := wanted[strings.ToLower(source.Platform)]; ok {
			kept = append(kept, source)
		}
	}

	return kept
}

// groupByCompany collapses sources into one entry per company, sorted by name.
//
// Grouping is case-insensitive because the registry deduplicates that way: the
// same employer is registered as "Oxide" on one platform and "oxide" on another,
// and listing it twice would be noise.
func groupByCompany(sources []Source) []companyEntry {
	grouped := make(map[string]*companyEntry)

	for _, source := range sources {
		key := strings.ToLower(source.Company)

		entry, ok := grouped[key]
		if !ok {
			entry = &companyEntry{Company: source.Company}
			grouped[key] = entry
		}

		if !slices.Contains(entry.Platforms, source.Platform) {
			entry.Platforms = append(entry.Platforms, source.Platform)
		}

		if !slices.Contains(entry.Keys, source.Key) {
			entry.Keys = append(entry.Keys, source.Key)
		}
	}

	entries := make([]companyEntry, 0, len(grouped))

	// Iterating the sorted key set rather than the map: map order is random per
	// run, and this list is part of a tool's answer.
	for _, key := range slices.Sorted(maps.Keys(grouped)) {
		entry := grouped[key]

		slices.Sort(entry.Platforms)
		slices.Sort(entry.Keys)

		entries = append(entries, *entry)
	}

	return entries
}

// platformEntry is one ATS platform and its coverage.
type platformEntry struct {
	Platform string `json:"platform"`

	// Sources counts job boards and Companies distinct employers. They differ
	// wherever one employer has several tenants on the same ATS.
	Sources   int `json:"sources"`
	Companies int `json:"companies"`
}

// platformListResult is the structured payload of list_platforms.
type platformListResult struct {
	Platforms      []platformEntry `json:"platforms"`
	TotalSources   int             `json:"total_sources"`
	TotalCompanies int             `json:"total_companies"`
}

// listPlatforms reports the ATS platforms covered. It makes no requests.
func (s *Server) listPlatforms(raw json.RawMessage) (any, *rpcError) {
	// Decoded despite taking no arguments, so that a client sending a stray
	// field is told rather than silently ignored.
	var args struct{}

	if rpcErr := decodeArgs(raw, &args); rpcErr != nil {
		return nil, rpcErr
	}

	var (
		sources   = s.Catalog.Sources()
		counts    = make(map[string]int)
		companies = make(map[string]map[string]struct{})
	)

	for _, source := range sources {
		counts[source.Platform]++

		if companies[source.Platform] == nil {
			companies[source.Platform] = make(map[string]struct{})
		}

		companies[source.Platform][strings.ToLower(source.Company)] = struct{}{}
	}

	entries := make([]platformEntry, 0, len(counts))

	for _, platform := range slices.Sorted(maps.Keys(counts)) {
		entries = append(entries, platformEntry{
			Platform:  platform,
			Sources:   counts[platform],
			Companies: len(companies[platform]),
		})
	}

	return succeed(platformListResult{
		Platforms:      entries,
		TotalSources:   len(sources),
		TotalCompanies: len(groupByCompany(sources)),
	})
}
