// Package query is the filter vocabulary every surface of this project shares:
// the CLI's flags, and whatever a TUI, an MCP tool or a browser build asks
// next. One query language, defined once, rather than three that drift.
//
// # Compatibility
//
// This package is pre-v1 and carries no stability guarantee. Its Go API may
// change in any release, with no deprecation period and no major-version bump.
// [Query]'s fields in particular are expected to grow. Pin a commit if you need
// one. See docs/design/package-taxonomy.md §7.
//
// # Dependencies
//
// This package imports the standard library and
// [github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting], and
// deliberately not net/http. Filtering a stored corpus must not link an HTTP
// stack.
package query

import (
	"slices"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// Query selects job postings of interest.
//
// A crawl covers well over a thousand companies and can return six figures of
// postings, so filtering is what makes the output usable. The zero value
// matches everything.
//
// Within a field, values are OR-ed: Titles{"security", "appsec"} matches a
// posting whose title contains either. Across fields they are AND-ed: a posting
// must match the title terms AND the location terms. Matching is
// case-insensitive substring matching, which suits the free-text fields job
// boards actually publish.
type Query struct {
	// Titles matches postings whose title contains any of these terms.
	Titles []string

	// ExcludeTitles rejects postings whose title contains any of these terms.
	// It is applied after Titles, so it can carve exceptions out of a match.
	ExcludeTitles []string

	// Locations matches postings whose location contains any of these terms.
	Locations []string

	// Companies matches postings from any of these companies.
	Companies []string

	// Remote matches only postings that appear to be remote.
	Remote bool

	// HasCompensation matches only postings that published a pay range.
	HasCompensation bool

	// MinAnnual matches only postings whose published pay reaches this annual
	// figure. Hourly and monthly ranges are annualized so they compare against
	// it, and the top of a range is what is compared.
	//
	// Setting this necessarily also requires published pay, so postings that
	// disclose nothing are excluded: most postings, on most boards. That is a
	// deliberate consequence: a pay floor cannot be applied to an unknown.
	MinAnnual float64

	// Departments matches postings whose department or team contains any of
	// these terms, with the same case-insensitive substring semantics as Titles.
	//
	// Both fields are searched because platforms disagree about which one holds
	// the word a person would type: Lever files "Engineering" under
	// categories.department and "Platform" under categories.team, Ashby publishes
	// both, and BambooHR has only a department. Requiring the user to know which
	// platform a company is on would make `--department engineering` return
	// nothing for half the crawl.
	Departments []string

	// EmploymentTypes matches postings whose employment type is any of these.
	//
	// Unlike the free-text fields this is equality against the normalized
	// vocabulary, not substring matching, because substrings are actively wrong
	// here: "contract" is a prefix of "contractor" and "temp" of "temporary", so
	// a substring filter would silently conflate categories the schema exists to
	// keep apart. Adapters normalize at their own boundary with
	// [jobposting.NormalizeEmploymentType]; filtering only compares.
	//
	// Postings whose board published no employment type are excluded, following
	// the precedent MinAnnual sets: a category filter cannot be applied to an
	// unknown.
	EmploymentTypes []jobposting.EmploymentType

	// WorkplaceTypes matches postings whose workplace type is any of these, by
	// equality, for the same reason as EmploymentTypes.
	//
	// When a posting carries no structured workplace type, remote and hybrid fall
	// back to the [jobposting.JobPosting.IsRemote] and
	// [jobposting.JobPosting.IsHybrid] heuristics over the location and title
	// text. Only a minority of adapters populate the structured field, and a
	// `--workplace-type remote` that returned almost nothing across a
	// 2,100-source crawl would look broken rather than precise. Onsite has no
	// fallback on purpose: the absence of the word "remote" is not evidence that
	// an employer requires an office.
	WorkplaceTypes []jobposting.WorkplaceType

	// PostedSince matches only postings the board published at or after this
	// instant.
	//
	// Postings with no publication date are excluded, again following MinAnnual:
	// most boards publish no date at all, and treating those as recent would
	// quietly fill a "last week" query with postings of unknown age. UpdatedAt is
	// deliberately not consulted — an employer editing a job description does not
	// make a nine-month-old req new.
	PostedSince time.Time
}

// Match reports whether a posting satisfies the query.
func (f Query) Match(j *jobposting.JobPosting) bool {
	if j == nil {
		return false
	}

	if f.Remote && !j.IsRemote() {
		return false
	}

	if f.HasCompensation && j.Compensation.IsZero() {
		return false
	}

	if f.MinAnnual > 0 {
		annual, ok := j.Compensation.AnnualMax()
		if !ok || annual < f.MinAnnual {
			return false
		}
	}

	if !matchesAny(j.Title, f.Titles) {
		return false
	}

	if len(f.ExcludeTitles) > 0 && containsAny(j.Title, f.ExcludeTitles) {
		return false
	}

	if !matchesAny(j.Location, f.Locations) {
		return false
	}

	if !matchesAny(j.Company, f.Companies) {
		return false
	}

	// Department and team are one constraint over two fields, so the terms are
	// OR-ed across both rather than AND-ed between them.
	if hasUsableTerm(f.Departments) &&
		!containsAny(j.Department, f.Departments) &&
		!containsAny(j.Team, f.Departments) {
		return false
	}

	if hasUsableEnum(f.EmploymentTypes) {
		// A posting whose board published no employment type cannot satisfy an
		// employment-type filter, the same way an undisclosed salary cannot
		// satisfy a pay floor.
		if j.EmploymentType == jobposting.EmploymentTypeUnknown ||
			!slices.Contains(f.EmploymentTypes, j.EmploymentType) {
			return false
		}
	}

	if hasUsableEnum(f.WorkplaceTypes) && !matchesAnyWorkplaceType(j, f.WorkplaceTypes) {
		return false
	}

	if !f.PostedSince.IsZero() {
		if j.PostedAt.IsZero() || j.PostedAt.Before(f.PostedSince) {
			return false
		}
	}

	return true
}

// matchesAnyWorkplaceType reports whether the posting satisfies any of the
// wanted workplace types.
//
// The board's own answer wins outright when it published one, exactly as
// [jobposting.JobPosting.IsRemote] prefers the structured remote flag over the
// location text. Only when the field is absent do remote and hybrid fall back to
// the text heuristics, which is what keeps the flag useful while adapters are
// still being migrated platform by platform.
func matchesAnyWorkplaceType(j *jobposting.JobPosting, wanted []jobposting.WorkplaceType) bool {
	if j.WorkplaceType != jobposting.WorkplaceTypeUnknown {
		return slices.Contains(wanted, j.WorkplaceType)
	}

	for _, want := range wanted {
		switch want {
		case jobposting.WorkplaceTypeRemote:
			if j.IsRemote() {
				return true
			}
		case jobposting.WorkplaceTypeHybrid:
			if j.IsHybrid() {
				return true
			}
		}
	}

	return false
}

// IsZero reports whether the query would match every posting, which lets
// callers skip filtering entirely. Term lists holding nothing but blanks count
// as absent, matching how [Query.Match] treats them.
//
// Every field of [Query] must be represented here. [Query.Apply] returns its
// input untouched when this reports true, so a field this function does not know
// about makes its flag silently match the entire crawl — a wrong answer with no
// error and no log line. TestFilterFieldsAreWiredIn walks the struct by
// reflection and fails when a field is missing from either this function or
// [Query.Match], so adding one without wiring it up cannot reach main.
func (f Query) IsZero() bool {
	return !f.Remote &&
		!f.HasCompensation &&
		f.MinAnnual <= 0 &&
		f.PostedSince.IsZero() &&
		!hasUsableTerm(f.Titles) &&
		!hasUsableTerm(f.ExcludeTitles) &&
		!hasUsableTerm(f.Locations) &&
		!hasUsableTerm(f.Companies) &&
		!hasUsableTerm(f.Departments) &&
		!hasUsableEnum(f.EmploymentTypes) &&
		!hasUsableEnum(f.WorkplaceTypes)
}

// hasUsableEnum reports whether a list of normalized vocabulary values
// constrains anything.
//
// An entry equal to the zero value is treated as absent rather than as "match
// postings the board said nothing about", for the same reason [matchesAny]
// treats a blank term as no constraint: an empty value arriving from a command
// line is a mistake, and answering a mistake with an empty result set reads as
// "nothing is hiring".
func hasUsableEnum[T ~string](values []T) bool {
	return slices.ContainsFunc(values, func(v T) bool { return v != "" })
}

// Apply returns the postings from jobs that match the query. Errors are passed
// through unchanged, so a filtered crawl still reports which sources failed.
func (f Query) Apply(jobs jobposting.Seq) jobposting.Seq {
	if f.IsZero() {
		return jobs
	}

	return func(yield func(*jobposting.JobPosting, error) bool) {
		for job, err := range jobs {
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}

			if !f.Match(job) {
				continue
			}

			if !yield(job, nil) {
				return
			}
		}
	}
}

// matchesAny reports whether value contains any of the terms, treating a list
// with no usable terms as "no constraint".
//
// A list of only blank terms counts as no constraint rather than as a filter
// nothing can satisfy. Otherwise `--title ""` would silently return zero
// postings, which reads as "nothing is hiring" rather than "that filter is
// empty".
func matchesAny(value string, terms []string) bool {
	if !hasUsableTerm(terms) {
		return true
	}

	return containsAny(value, terms)
}

// hasUsableTerm reports whether terms contains at least one non-blank entry.
func hasUsableTerm(terms []string) bool {
	for _, term := range terms {
		if strings.TrimSpace(term) != "" {
			return true
		}
	}

	return false
}

// containsAny reports whether value contains any of the terms,
// case-insensitively.
func containsAny(value string, terms []string) bool {
	value = strings.ToLower(value)

	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}

		if strings.Contains(value, term) {
			return true
		}
	}

	return false
}
