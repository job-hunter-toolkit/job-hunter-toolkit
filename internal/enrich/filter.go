package enrich

import "strings"

// Filter selects postings by what is known about the employer behind them,
// rather than by what the board published about the job.
//
// It is deliberately a separate type from [internal.Filter] and applies to a
// separate stream. Both halves of that separation matter: internal.Filter
// answers questions about a posting, this answers questions about a company,
// and mixing them would make a single --min-pay-shaped flag able to compare a
// published salary against a third-party benchmark. docs/compensation.md's
// "nothing blends sources" is enforced here by the type system rather than by
// remembering.
//
// Every field excludes postings whose employer is unknown, and that is not a
// bug to work around. An unmatched company is not a private company, is not a
// small company, and is not in no industry; it is a company nobody has resolved.
// Answering "is this employer public?" with "no" for 2,000 unresolved startups
// would be the plausible wrong answer this package exists to refuse.
//
// The zero value matches everything.
type Filter struct {
	// Known matches only postings whose employer has a reviewed row, which is
	// the honest way to ask "show me only what I have context for". It is the
	// analogue of internal.Filter.HasCompensation.
	Known bool

	// Industries matches employers whose industry description or SIC code
	// contains any of these terms, case-insensitively, matching the substring
	// semantics of every other free-text filter in this project.
	//
	// The SIC code is searched alongside the description so that `--industry
	// 7372` and `--industry software` both work; the four-digit codes are how
	// EDGAR actually classifies, and the descriptions are how people talk.
	Industries []string

	// Public matches only employers established to file with the SEC.
	Public bool

	// Private matches only employers established NOT to be public.
	//
	// This is not the complement of Public, and the difference is the whole
	// reason [Employer.Public] is a pointer. Not finding an EDGAR filing is not
	// evidence of being privately held, so an employer with no row and an
	// employer with public=false are different answers and only the second one
	// satisfies this.
	Private bool

	// MinEmployees matches only employers with at least this many employees.
	// Employers whose headcount was never resolved are excluded, following the
	// precedent internal.Filter.MinAnnual sets: a floor cannot be applied to an
	// unknown.
	MinEmployees int
}

// IsZero reports whether the filter would match every posting.
//
// Every field of [Filter] must be represented here. [Filter.Apply] returns its
// input untouched when this reports true, so a field this function does not know
// about makes its flag silently match the entire crawl — a wrong answer with no
// error and no log line. internal/filter.go documents having already fallen into
// exactly that trap once; TestEnrichFilterFieldsAreWiredIn walks this struct by
// reflection so it cannot happen again here.
func (f Filter) IsZero() bool {
	return !f.Known &&
		!f.Public &&
		!f.Private &&
		f.MinEmployees <= 0 &&
		!hasTerm(f.Industries)
}

// Match reports whether a posting satisfies the filter.
func (f Filter) Match(p *Posting) bool {
	if p == nil {
		return false
	}

	employer := p.Employer

	// Every constraint below is a statement about an employer, so an unknown
	// employer satisfies none of them. Checked once here rather than field by
	// field so a new field cannot forget it.
	if employer == nil {
		return f.IsZero()
	}

	if f.Public && (employer.Public == nil || !*employer.Public) {
		return false
	}

	if f.Private && (employer.Public == nil || *employer.Public) {
		return false
	}

	if f.MinEmployees > 0 && employer.Employees < f.MinEmployees {
		return false
	}

	if hasTerm(f.Industries) && !containsAnyTerm(employer.Industry, f.Industries) &&
		!containsAnyTerm(employer.SIC, f.Industries) {
		return false
	}

	// Known needs no check of its own: reaching this line already means the
	// employer was found, which is the whole of what it asks. It is a named
	// field rather than an implied one because "only postings I have context
	// for" is a question worth being able to ask by itself.
	return true
}

// Apply returns the postings that match the filter.
//
// Errors pass through unchanged, so a filtered crawl still reports which sources
// failed. Suppressing them here would let an enrichment flag turn a broken
// source into a silently missing one, which is the "a failed source cannot make
// previously seen jobs look removed" invariant.
func (f Filter) Apply(postings Postings) Postings {
	if f.IsZero() {
		return postings
	}

	return func(yield func(*Posting, error) bool) {
		for posting, err := range postings {
			if err != nil {
				if !yield(nil, err) {
					return
				}

				continue
			}

			if !f.Match(posting) {
				continue
			}

			if !yield(posting, nil) {
				return
			}
		}
	}
}

// hasTerm reports whether terms holds at least one non-blank entry, treating a
// list of blanks as no constraint for the same reason internal/filter.go does:
// `--industry ""` returning nothing reads as "nothing is hiring" rather than as
// "that filter is empty".
func hasTerm(terms []string) bool {
	for _, term := range terms {
		if strings.TrimSpace(term) != "" {
			return true
		}
	}

	return false
}

// containsAnyTerm reports whether value contains any of the terms,
// case-insensitively.
func containsAnyTerm(value string, terms []string) bool {
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
