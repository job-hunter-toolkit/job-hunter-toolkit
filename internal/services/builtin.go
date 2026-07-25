package services

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// Source is a single crawlable job board: one company on one ATS.
type Source struct {
	// Key identifies the company to its ATS and is what the adapter fetches
	// with: a board slug for most platforms, a full tenant URL for Workday, a
	// hostname for Phenom.
	Key string

	// Company is the human-facing name, derived from Key. For most platforms it
	// is the same string; where the ATS uses a URL or hostname as its key, this
	// is the short name a person would recognise.
	//
	// Keeping these separate matters. Conflating them put raw tenant URLs into
	// the user-facing company list, where they sorted under "https://" instead of
	// alphabetically, and made `--company <tenant URL>` silently return zero
	// postings because the filter compared a URL against a short name.
	Company string

	// Jobs fetches this company's postings.
	Jobs internal.JobsFunc
}

// Builtin holds every job source compiled into this binary.
//
// It contains one entry per company rather than one per ATS. That granularity is
// deliberate: [internal.All] schedules sources concurrently, so splitting per
// company lets a crawl fetch many companies at once instead of serialising every
// company behind its platform.
var Builtin []Source

// registerBuiltin adds sources to [Builtin]. It is called from each service
// file's init function.
func registerBuiltin(sources []Source) {
	Builtin = append(Builtin, sources...)
}

// JobsFuncs returns the fetch function of each given source, ready to pass to
// [internal.All].
func JobsFuncs(sources []Source) []internal.JobsFunc {
	funcs := make([]internal.JobsFunc, 0, len(sources))

	for _, source := range sources {
		funcs = append(funcs, source.Jobs)
	}

	return funcs
}

// SourcesMatching returns the builtin sources whose company identifier contains
// any of the given terms, case-insensitively. With no terms it returns every
// source.
//
// This narrows the crawl itself rather than filtering its output, which is the
// difference between a targeted query taking seconds and taking as long as a
// full crawl of every company.
// Terms are matched against both the display name and the ATS key, so both
// `--company pfizer` and `--company pfizer.wd1.myworkdayjobs.com` select the same
// source. Matching only one of the two made whichever form the user did not
// happen to pick silently select nothing.
func SourcesMatching(terms []string) []Source {
	lowered := make([]string, 0, len(terms))

	for _, term := range terms {
		if term = strings.ToLower(strings.TrimSpace(term)); term != "" {
			lowered = append(lowered, term)
		}
	}

	if len(lowered) == 0 {
		// Clipped so a caller appending to the result reallocates instead of
		// writing into the shared registry's backing array.
		return slices.Clip(Builtin)
	}

	var matched []Source

	for _, source := range Builtin {
		var (
			company = strings.ToLower(source.Company)
			key     = strings.ToLower(source.Key)
		)

		for _, term := range lowered {
			if strings.Contains(company, term) || strings.Contains(key, term) {
				matched = append(matched, source)

				break
			}
		}
	}

	return matched
}

// Companies returns the identifier of every company this binary can crawl,
// deduplicated and sorted. Several companies appear on more than one platform,
// hence the deduplication.
func Companies() []string {
	companies := make([]string, 0, len(Builtin))

	for _, source := range Builtin {
		companies = append(companies, source.Company)
	}

	slices.SortFunc(companies, func(a, b string) int {
		return cmp.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	// Deduplicated case-insensitively to match the sort: a company can be
	// registered on two platforms with different slug casing, and listing it
	// twice would be noise.
	return slices.CompactFunc(companies, strings.EqualFold)
}

// companyJobsFunc fetches postings for a single company from one ATS.
type companyJobsFunc func(ctx context.Context, httpClient *http.Client, companyOrURL string) internal.Jobs

// multiJobsFunc adapts a per-company fetch function into one source per
// company, so each company can be scheduled independently.
//
// It is for platforms whose key is already a readable company name, which is
// most of them. Platforms keyed by a URL or hostname should use
// [multiJobsFuncNamed] so their display name is not a URL.
func multiJobsFunc(sourceJobFunc companyJobsFunc, keys []string) []Source {
	return multiJobsFuncNamed(sourceJobFunc, keys, func(key string) string { return key })
}

// multiJobsFuncNamed is [multiJobsFunc] for platforms whose key is not itself a
// readable name, deriving the display name with companyName.
func multiJobsFuncNamed(sourceJobFunc companyJobsFunc, keys []string, companyName func(string) string) []Source {
	sources := make([]Source, 0, len(keys))

	for _, companyOrURL := range keys {
		sources = append(sources, Source{
			Key:     companyOrURL,
			Company: companyName(companyOrURL),
			Jobs: func(ctx context.Context, httpClient *http.Client) internal.Jobs {
				return func(yield func(*internal.JobPosting, error) bool) {
					for jobPosting, err := range sourceJobFunc(ctx, httpClient, companyOrURL) {
						if ctx.Err() != nil {
							yield(nil, ctx.Err())
							return
						}

						if !yield(jobPosting, err) {
							return
						}
					}
				}
			},
		})
	}

	return sources
}
