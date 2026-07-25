package internal_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

func TestIsRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
		title    string
		want     bool
	}{
		{name: "plain remote", location: "Remote", want: true},
		{name: "remote with region", location: "Remote - US", want: true},
		{name: "us remote", location: "US Remote", want: true},
		{name: "remote parenthetical", location: "Remote (Europe)", want: true},
		{name: "anywhere", location: "Anywhere", want: true},
		{name: "work from home", location: "Work From Home", want: true},
		{name: "telecommute", location: "Telecommute, USA", want: true},
		{name: "distributed", location: "Distributed team", want: true},
		{name: "remote in title", location: "", title: "Security Engineer (Remote)", want: true},
		{name: "lowercase", location: "remote", want: true},

		{name: "office city", location: "San Francisco, CA", want: false},
		{name: "empty", location: "", want: false},
		{name: "unknown placeholder", location: "unknown/remote", want: true},

		// Oregon's postal abbreviation is OR, which must not be mistaken for a
		// remote marker.
		{name: "oregon city", location: "Bend, OR", want: false},
		{name: "portland", location: "Portland, OR", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := &internal.JobPosting{Location: tt.location, Title: tt.title}

			if got := job.IsRemote(); got != tt.want {
				t.Errorf("IsRemote() with location %q title %q = %v, want %v",
					tt.location, tt.title, got, tt.want)
			}
		})
	}
}

func TestIsRemoteNilSafe(t *testing.T) {
	t.Parallel()

	var job *internal.JobPosting

	if job.IsRemote() {
		t.Error("IsRemote() on nil = true, want false")
	}

	if job.IsHybrid() {
		t.Error("IsHybrid() on nil = true, want false")
	}
}

func TestIsHybrid(t *testing.T) {
	t.Parallel()

	hybrid := &internal.JobPosting{Location: "Hybrid - Austin, TX"}
	if !hybrid.IsHybrid() {
		t.Error("IsHybrid() = false, want true")
	}

	remote := &internal.JobPosting{Location: "Remote"}
	if remote.IsHybrid() {
		t.Error("IsHybrid() on a remote posting = true, want false")
	}
}

func TestFilterZeroValueMatchesEverything(t *testing.T) {
	t.Parallel()

	var f internal.Filter

	if !f.IsZero() {
		t.Error("IsZero() = false, want true for the zero value")
	}

	job := &internal.JobPosting{Company: "acme", Title: "Chef", Location: "Paris"}
	if !f.Match(job) {
		t.Error("Match() = false, want true for the zero-value filter")
	}
}

func TestFilterMatch(t *testing.T) {
	t.Parallel()

	job := &internal.JobPosting{
		Company:  "acme",
		Title:    "Senior Application Security Engineer",
		Location: "Remote - US",
		URL:      "https://example.test/1",
	}

	tests := []struct {
		name   string
		filter internal.Filter
		want   bool
	}{
		{
			name:   "title substring",
			filter: internal.Filter{Titles: []string{"security"}},
			want:   true,
		},
		{
			name:   "title is case insensitive",
			filter: internal.Filter{Titles: []string{"SECURITY"}},
			want:   true,
		},
		{
			name:   "title terms are or-ed",
			filter: internal.Filter{Titles: []string{"marketing", "security"}},
			want:   true,
		},
		{
			name:   "title miss",
			filter: internal.Filter{Titles: []string{"marketing"}},
			want:   false,
		},
		{
			name:   "excluded title",
			filter: internal.Filter{Titles: []string{"engineer"}, ExcludeTitles: []string{"senior"}},
			want:   false,
		},
		{
			name:   "exclude that does not apply",
			filter: internal.Filter{Titles: []string{"engineer"}, ExcludeTitles: []string{"principal"}},
			want:   true,
		},
		{
			name:   "remote only",
			filter: internal.Filter{Remote: true},
			want:   true,
		},
		{
			name:   "location match",
			filter: internal.Filter{Locations: []string{"us"}},
			want:   true,
		},
		{
			name:   "company match",
			filter: internal.Filter{Companies: []string{"acme"}},
			want:   true,
		},
		{
			name:   "company miss",
			filter: internal.Filter{Companies: []string{"globex"}},
			want:   false,
		},
		{
			name: "fields are and-ed",
			filter: internal.Filter{
				Titles:    []string{"security"},
				Companies: []string{"globex"},
			},
			want: false,
		},
		{
			name: "all fields match",
			filter: internal.Filter{
				Titles:    []string{"security"},
				Companies: []string{"acme"},
				Locations: []string{"remote"},
				Remote:    true,
			},
			want: true,
		},
		{
			// A filter of only blank terms is an empty filter, not one that
			// nothing can satisfy: `--title ""` must not silently return zero
			// postings.
			name:   "only blank terms is no constraint",
			filter: internal.Filter{Titles: []string{"", "  "}},
			want:   true,
		},
		{
			name:   "blank terms alongside a real term are skipped",
			filter: internal.Filter{Titles: []string{"", "security"}},
			want:   true,
		},
		{
			name:   "blank terms do not rescue a non-matching term",
			filter: internal.Filter{Titles: []string{"", "marketing"}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.filter.Match(job); got != tt.want {
				t.Errorf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterRejectsNonRemoteWhenRemoteRequired(t *testing.T) {
	t.Parallel()

	onsite := &internal.JobPosting{Title: "Security Engineer", Location: "Austin, TX"}

	if (internal.Filter{Remote: true}).Match(onsite) {
		t.Error("Match() = true, want false for an onsite posting")
	}
}

func TestFilterApplyPassesErrorsThrough(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("board unavailable")

	jobs := internal.Jobs(func(yield func(*internal.JobPosting, error) bool) {
		yield(&internal.JobPosting{Title: "Security Engineer", Location: "Remote"}, nil)
		yield(nil, wantErr)
		yield(&internal.JobPosting{Title: "Marketing Lead", Location: "Remote"}, nil)
	})

	filtered := internal.Filter{Titles: []string{"security"}}.Apply(jobs)

	var (
		titles []string
		errs   []error
	)

	for job, err := range filtered {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		titles = append(titles, job.Title)
	}

	if want := []string{"Security Engineer"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v", titles, want)
	}

	// Source failures must survive filtering, or a filtered crawl would hide
	// which sources are broken.
	if len(errs) != 1 || !errors.Is(errs[0], wantErr) {
		t.Errorf("errs = %v, want the source error passed through", errs)
	}
}

func TestFilterApplyIsPassthroughWhenZero(t *testing.T) {
	t.Parallel()

	jobs := internal.Jobs(func(yield func(*internal.JobPosting, error) bool) {
		yield(&internal.JobPosting{Title: "One"}, nil)
		yield(&internal.JobPosting{Title: "Two"}, nil)
	})

	var got []string
	for job := range (internal.Filter{}).Apply(jobs) {
		got = append(got, job.Title)
	}

	if want := []string{"One", "Two"}; !slices.Equal(got, want) {
		t.Errorf("titles = %v, want %v", got, want)
	}
}

func TestFilterApplyStopsEarly(t *testing.T) {
	t.Parallel()

	yielded := 0

	jobs := internal.Jobs(func(yield func(*internal.JobPosting, error) bool) {
		for range 100 {
			yielded++
			if !yield(&internal.JobPosting{Title: "Security Engineer"}, nil) {
				return
			}
		}
	})

	for range (internal.Filter{Titles: []string{"security"}}).Apply(jobs) {
		break
	}

	if yielded != 1 {
		t.Errorf("source yielded %d postings, want 1 (early stop must propagate)", yielded)
	}
}

func TestDedupe(t *testing.T) {
	t.Parallel()

	jobs := internal.Jobs(func(yield func(*internal.JobPosting, error) bool) {
		yield(&internal.JobPosting{Company: "acme", Title: "A", URL: "https://x.test/1"}, nil)
		// Same URL under a different company slug: the same job listed twice.
		yield(&internal.JobPosting{Company: "acme-inc", Title: "A", URL: "https://x.test/1"}, nil)
		yield(&internal.JobPosting{Company: "acme", Title: "B", URL: "https://x.test/2"}, nil)
	})

	var titles []string
	for job, err := range internal.Dedupe(jobs) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		titles = append(titles, job.Title)
	}

	if want := []string{"A", "B"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v", titles, want)
	}
}

func TestDedupeFallsBackWhenURLMissing(t *testing.T) {
	t.Parallel()

	jobs := internal.Jobs(func(yield func(*internal.JobPosting, error) bool) {
		yield(&internal.JobPosting{Company: "acme", Title: "A", Location: "Remote"}, nil)
		yield(&internal.JobPosting{Company: "acme", Title: "A", Location: "Remote"}, nil)
		// Same title, different location: a genuinely distinct posting.
		yield(&internal.JobPosting{Company: "acme", Title: "A", Location: "Austin"}, nil)
	})

	count := 0
	for range internal.Dedupe(jobs) {
		count++
	}

	if count != 2 {
		t.Errorf("got %d postings, want 2", count)
	}
}

func TestDedupePassesErrorsThrough(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")

	jobs := internal.Jobs(func(yield func(*internal.JobPosting, error) bool) {
		yield(nil, wantErr)
		yield(nil, wantErr)
	})

	errs := 0
	for _, err := range internal.Dedupe(jobs) {
		if err != nil {
			errs++
		}
	}

	// Errors are never deduplicated: two failing sources are two facts.
	if errs != 2 {
		t.Errorf("got %d errors, want 2", errs)
	}
}

func TestFilterIsZeroTreatsBlankTermsAsAbsent(t *testing.T) {
	t.Parallel()

	// Consistency with Match: a filter of only blank terms constrains nothing,
	// so it should also report itself as zero and let callers skip filtering.
	blank := internal.Filter{
		Titles:    []string{"", "   "},
		Locations: []string{" "},
	}

	if !blank.IsZero() {
		t.Error("IsZero() = false, want true for a filter of only blank terms")
	}

	real := internal.Filter{Titles: []string{"", "security"}}
	if real.IsZero() {
		t.Error("IsZero() = true, want false when a usable term is present")
	}
}

func TestCompensationAnnualMax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		comp *internal.Compensation
		want float64
		ok   bool
	}{
		{
			name: "annual salary",
			comp: &internal.Compensation{Min: 160000, Max: 185000, Period: internal.PeriodYear},
			want: 185000,
			ok:   true,
		},
		{
			// The point of annualizing: an hourly retail range has to be
			// comparable with a salaried one for a single pay filter to work.
			name: "hourly rate is annualized",
			comp: &internal.Compensation{Min: 13.66, Max: 19.16, Period: internal.PeriodHour},
			want: 19.16 * 2080,
			ok:   true,
		},
		{
			name: "monthly rate is annualized",
			comp: &internal.Compensation{Max: 5000, Period: internal.PeriodMonth},
			want: 60000,
			ok:   true,
		},
		{
			// Boards that omit the period are quoting hourly rates; no real
			// annual salary is under the ceiling.
			name: "unlabelled small figure is treated as hourly",
			comp: &internal.Compensation{Min: 18, Max: 24},
			want: 24 * 2080,
			ok:   true,
		},
		{
			name: "unlabelled large figure is treated as annual",
			comp: &internal.Compensation{Min: 120000, Max: 150000},
			want: 150000,
			ok:   true,
		},
		{
			name: "only a minimum published",
			comp: &internal.Compensation{Min: 90000, Period: internal.PeriodYear},
			want: 90000,
			ok:   true,
		},
		{
			name: "nothing published",
			comp: &internal.Compensation{Summary: "Competitive"},
			ok:   false,
		},
		{
			name: "nil",
			comp: nil,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tt.comp.AnnualMax()

			if ok != tt.ok {
				t.Fatalf("AnnualMax() ok = %v, want %v", ok, tt.ok)
			}

			if ok && got != tt.want {
				t.Errorf("AnnualMax() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompensationIsZero(t *testing.T) {
	t.Parallel()

	var nilComp *internal.Compensation
	if !nilComp.IsZero() {
		t.Error("IsZero() on nil = false, want true")
	}

	if !(&internal.Compensation{}).IsZero() {
		t.Error("IsZero() on empty = false, want true")
	}

	// A summary with no numbers is still information worth keeping.
	if (&internal.Compensation{Summary: "Offers Equity"}).IsZero() {
		t.Error("IsZero() with a summary = true, want false")
	}

	if (&internal.Compensation{Max: 1}).IsZero() {
		t.Error("IsZero() with an amount = true, want false")
	}
}

func TestFilterMinAnnual(t *testing.T) {
	t.Parallel()

	salaried := &internal.JobPosting{
		Title:        "Staff Security Engineer",
		Compensation: &internal.Compensation{Min: 200000, Max: 260000, Period: internal.PeriodYear},
	}
	hourly := &internal.JobPosting{
		Title:        "Pet Groomer",
		Compensation: &internal.Compensation{Min: 17.17, Max: 29.95, Period: internal.PeriodHour},
	}
	undisclosed := &internal.JobPosting{Title: "Security Engineer"}

	if !(internal.Filter{MinAnnual: 180000}).Match(salaried) {
		t.Error("salaried posting above the floor was rejected")
	}

	if (internal.Filter{MinAnnual: 300000}).Match(salaried) {
		t.Error("salaried posting below the floor was accepted")
	}

	// 29.95/hour annualizes to about 62k, so it clears a 60k floor.
	if !(internal.Filter{MinAnnual: 60000}).Match(hourly) {
		t.Error("hourly posting above the annualized floor was rejected")
	}

	if (internal.Filter{MinAnnual: 70000}).Match(hourly) {
		t.Error("hourly posting below the annualized floor was accepted")
	}

	// A pay floor cannot be applied to an undisclosed salary, so those are
	// excluded rather than assumed to qualify.
	if (internal.Filter{MinAnnual: 1}).Match(undisclosed) {
		t.Error("posting with no published pay was accepted by a pay floor")
	}
}

func TestFilterHasCompensation(t *testing.T) {
	t.Parallel()

	withPay := &internal.JobPosting{Compensation: &internal.Compensation{Max: 100000}}
	without := &internal.JobPosting{}

	if !(internal.Filter{HasCompensation: true}).Match(withPay) {
		t.Error("posting with pay was rejected")
	}

	if (internal.Filter{HasCompensation: true}).Match(without) {
		t.Error("posting without pay was accepted")
	}

	if (internal.Filter{HasCompensation: true}).IsZero() {
		t.Error("IsZero() = true, want false when a pay constraint is set")
	}

	if (internal.Filter{MinAnnual: 1000}).IsZero() {
		t.Error("IsZero() = true, want false when a pay floor is set")
	}
}

func TestIsRemotePrefersStructuredFlag(t *testing.T) {
	t.Parallel()

	// When a board publishes a real remote flag, it beats guessing from text.
	yes, no := true, false

	onsiteText := &internal.JobPosting{Location: "New York", Remote: &yes}
	if !onsiteText.IsRemote() {
		t.Error("structured remote=true was ignored in favour of the location text")
	}

	remoteText := &internal.JobPosting{Location: "Remote - US", Remote: &no}
	if remoteText.IsRemote() {
		t.Error("structured remote=false was overridden by the location text")
	}

	// With no flag, the heuristic still applies.
	if !(&internal.JobPosting{Location: "Remote"}).IsRemote() {
		t.Error("heuristic failed when no structured flag was present")
	}
}
