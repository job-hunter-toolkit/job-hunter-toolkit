package internal

import (
	"strings"
)

// Filter selects job postings of interest.
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
type Filter struct {
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
}

// remoteTerms are the phrases job boards use to indicate a role is not
// tied to an office. They are matched against both location and title, because
// boards are inconsistent about which field carries the signal.
var remoteTerms = []string{
	"remote",
	"anywhere",
	"work from home",
	"work-from-home",
	"wfh",
	"telecommute",
	"distributed",
	"virtual",
	"home based",
	"home-based",
}

// IsRemote reports whether a posting looks remote.
//
// When the board published a structured remote flag, that answer is used. Most
// boards do not, so otherwise this falls back to a heuristic over the location
// and title text. The heuristic errs toward inclusion, since a false positive is
// cheap to skim past and a false negative hides a job.
func (j *JobPosting) IsRemote() bool {
	if j == nil {
		return false
	}

	// The board's own answer beats guessing from free text.
	if j.Remote != nil {
		return *j.Remote
	}

	location := strings.ToLower(j.Location)
	title := strings.ToLower(j.Title)

	for _, term := range remoteTerms {
		if strings.Contains(location, term) || strings.Contains(title, term) {
			return true
		}
	}

	return false
}

// IsHybrid reports whether a posting describes itself as hybrid, meaning it
// expects some office presence. Hybrid roles frequently also say "remote", so
// callers wanting fully-remote work should check this too.
func (j *JobPosting) IsHybrid() bool {
	if j == nil {
		return false
	}

	return strings.Contains(strings.ToLower(j.Location), "hybrid") ||
		strings.Contains(strings.ToLower(j.Title), "hybrid")
}

// Match reports whether a posting satisfies the filter.
func (f Filter) Match(j *JobPosting) bool {
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

	return true
}

// IsZero reports whether the filter would match every posting, which lets
// callers skip filtering entirely. Term lists holding nothing but blanks count
// as absent, matching how [Filter.Match] treats them.
func (f Filter) IsZero() bool {
	return !f.Remote &&
		!f.HasCompensation &&
		f.MinAnnual <= 0 &&
		!hasUsableTerm(f.Titles) &&
		!hasUsableTerm(f.ExcludeTitles) &&
		!hasUsableTerm(f.Locations) &&
		!hasUsableTerm(f.Companies)
}

// Apply returns the postings from jobs that match the filter. Errors are passed
// through unchanged, so a filtered crawl still reports which sources failed.
func (f Filter) Apply(jobs Jobs) Jobs {
	if f.IsZero() {
		return jobs
	}

	return func(yield func(*JobPosting, error) bool) {
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

// Dedupe returns the postings from jobs with duplicates removed.
//
// Duplicates are common in practice: a company can appear under more than one
// ATS slug, and boards sometimes list the same role in several locations.
// Postings are considered the same when they share a URL, since that is the
// identity a job seeker actually cares about.
//
// Errors pass through unchanged and are never deduplicated.
func Dedupe(jobs Jobs) Jobs {
	return func(yield func(*JobPosting, error) bool) {
		seen := make(map[string]struct{})

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

			key := job.URL
			if key == "" {
				// Without a URL there is no stable identity, so fall back to the
				// posting's descriptive fields.
				key = job.Company + "\x00" + job.Title + "\x00" + job.Location
			}

			if _, ok := seen[key]; ok {
				continue
			}

			seen[key] = struct{}{}

			if !yield(job, nil) {
				return
			}
		}
	}
}
