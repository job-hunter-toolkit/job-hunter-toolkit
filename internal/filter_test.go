package internal_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
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

// constrainingTerm is a term no posting in these tests contains, so a filter
// built from it constrains something no matter which field it lands in.
const constrainingTerm = "zzz-no-posting-says-this"

// constrainingInstant is a non-zero cutoff for time-valued filter fields.
var constrainingInstant = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fieldsSatisfiedByAnEmptyPosting names the [internal.Filter] fields whose
// constraint a posting carrying no data genuinely meets, and says why. Every
// other field must reject one.
var fieldsSatisfiedByAnEmptyPosting = map[string]string{
	"ExcludeTitles": "an exclusion is satisfied by a posting containing none of its terms",
}

// TestFilterFieldsAreWiredIn is the guard for the trap this filter has already
// fallen into once.
//
// [internal.Filter.Apply] returns its input untouched when IsZero reports true,
// and IsZero enumerates its fields by hand. A field added to the struct and to
// the CLI but forgotten in IsZero makes its flag match the entire crawl: no
// error, no log line, just a wrong answer at 473,000 postings of scale. A field
// forgotten in Match does the same thing one layer down.
//
// So this walks the struct by reflection instead of trusting anyone to remember.
// A new field fails here until it is wired into both, and a field whose type this
// test cannot build fails with instructions rather than passing vacuously.
func TestFilterFieldsAreWiredIn(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[internal.Filter]()

	for i := range typ.NumField() {
		field := typ.Field(i)

		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()

			must.True(t, field.IsExported(), must.Sprintf(
				"Filter.%s is unexported, so this reflection guard cannot set it; "+
					"cover it with an equivalent test inside package internal",
				field.Name))

			value := reflect.New(typ).Elem()
			value.Field(i).Set(constrainingValue(t, field.Type))

			filter, ok := value.Interface().(internal.Filter)
			must.True(t, ok)

			must.False(t, filter.IsZero(), must.Sprintf(
				"Filter.IsZero() ignores %s, so a filter setting only that field would pass "+
					"straight through Apply and silently match every posting; add %s to IsZero",
				field.Name, field.Name))

			if why, expected := fieldsSatisfiedByAnEmptyPosting[field.Name]; expected {
				t.Logf("Filter.%s may match an empty posting: %s", field.Name, why)

				return
			}

			must.False(t, filter.Match(&internal.JobPosting{}), must.Sprintf(
				"Filter.Match() ignores %s: a posting with no data satisfied a constraint on it. "+
					"Wire the field into Match, or record it in fieldsSatisfiedByAnEmptyPosting with a reason",
				field.Name))
		})
	}
}

// constrainingValue builds a value for a filter field that is guaranteed to
// constrain something, for whichever type a future field turns out to have.
func constrainingValue(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()

	// time.Time is a struct with unexported fields, so it needs naming rather
	// than deriving.
	if typ == reflect.TypeFor[time.Time]() {
		return reflect.ValueOf(constrainingInstant)
	}

	value := reflect.New(typ).Elem()

	switch typ.Kind() {
	case reflect.Bool:
		value.SetBool(true)
	case reflect.String:
		value.SetString(constrainingTerm)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(1)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(1)
	case reflect.Slice:
		return reflect.Append(reflect.MakeSlice(typ, 0, 1), constrainingValue(t, typ.Elem()))
	case reflect.Pointer:
		pointer := reflect.New(typ.Elem())
		pointer.Elem().Set(constrainingValue(t, typ.Elem()))

		return pointer
	default:
		t.Fatalf("no constraining value known for filter field type %s; "+
			"teach constrainingValue about it, and check Filter.IsZero and Filter.Match handle it", typ)
	}

	return value
}

func TestFilterDepartments(t *testing.T) {
	t.Parallel()

	// Department and team are searched together because platforms disagree about
	// which one holds the word a person would type: Lever files "Engineering"
	// under categories.department and "Platform" under categories.team.
	byDepartment := &internal.JobPosting{Title: "Engineer", Department: "Engineering", Team: "Platform"}
	byTeamOnly := &internal.JobPosting{Title: "Engineer", Team: "Security Engineering"}
	unlabelled := &internal.JobPosting{Title: "Engineer"}

	test.True(t, internal.Filter{Departments: []string{"engineering"}}.Match(byDepartment))
	test.True(t, internal.Filter{Departments: []string{"platform"}}.Match(byDepartment))
	test.True(t, internal.Filter{Departments: []string{"engineering"}}.Match(byTeamOnly))
	test.False(t, internal.Filter{Departments: []string{"marketing"}}.Match(byDepartment))

	// Boards that publish no department cannot satisfy a department filter.
	test.False(t, internal.Filter{Departments: []string{"engineering"}}.Match(unlabelled))

	// Terms within the flag are OR-ed, and the flag is AND-ed with the others.
	test.True(t, internal.Filter{Departments: []string{"marketing", "platform"}}.Match(byDepartment))
	test.False(t, internal.Filter{
		Departments: []string{"engineering"},
		Titles:      []string{"recruiter"},
	}.Match(byDepartment))

	test.False(t, internal.Filter{Departments: []string{"engineering"}}.IsZero())
	test.True(t, internal.Filter{Departments: []string{"", "  "}}.IsZero())
}

func TestFilterEmploymentType(t *testing.T) {
	t.Parallel()

	fullTime := &internal.JobPosting{Title: "Engineer", EmploymentType: internal.EmploymentTypeFullTime}
	contract := &internal.JobPosting{Title: "Engineer", EmploymentType: internal.EmploymentTypeContract}
	unlabelled := &internal.JobPosting{Title: "Engineer"}

	onlyContract := internal.Filter{EmploymentTypes: []internal.EmploymentType{internal.EmploymentTypeContract}}

	test.True(t, onlyContract.Match(contract))
	test.False(t, onlyContract.Match(fullTime))

	// Matching is equality against the normalized vocabulary, never substring:
	// "contract" is a prefix of "contractor" and "intern" of "internship", so a
	// substring filter would merge the categories this schema exists to separate.
	test.False(t, internal.Filter{
		EmploymentTypes: []internal.EmploymentType{"contractor"},
	}.Match(contract))

	// A posting whose board published nothing is excluded, following the
	// precedent MinAnnual sets for undisclosed pay.
	test.False(t, onlyContract.Match(unlabelled))

	either := internal.Filter{EmploymentTypes: []internal.EmploymentType{
		internal.EmploymentTypeContract,
		internal.EmploymentTypeFullTime,
	}}

	test.True(t, either.Match(fullTime))
	test.True(t, either.Match(contract))

	test.False(t, onlyContract.IsZero())
	test.True(t, internal.Filter{EmploymentTypes: []internal.EmploymentType{""}}.IsZero())
}

func TestFilterWorkplaceType(t *testing.T) {
	t.Parallel()

	remote := internal.Filter{WorkplaceTypes: []internal.WorkplaceType{internal.WorkplaceTypeRemote}}
	hybrid := internal.Filter{WorkplaceTypes: []internal.WorkplaceType{internal.WorkplaceTypeHybrid}}
	onsite := internal.Filter{WorkplaceTypes: []internal.WorkplaceType{internal.WorkplaceTypeOnsite}}

	structuredRemote := &internal.JobPosting{Location: "New York", WorkplaceType: internal.WorkplaceTypeRemote}
	structuredOnsite := &internal.JobPosting{Location: "Remote-friendly office", WorkplaceType: internal.WorkplaceTypeOnsite}

	// The board's own answer beats the location text, exactly as it does for the
	// structured remote flag.
	test.True(t, remote.Match(structuredRemote))
	test.False(t, onsite.Match(structuredRemote))
	test.True(t, onsite.Match(structuredOnsite))
	test.False(t, remote.Match(structuredOnsite))

	// Only a minority of adapters publish the structured field, so remote and
	// hybrid fall back to the text heuristics rather than reporting almost
	// nothing across a 1,900-source crawl.
	test.True(t, remote.Match(&internal.JobPosting{Location: "Remote - US"}))
	test.True(t, hybrid.Match(&internal.JobPosting{Location: "Hybrid - Austin, TX"}))
	test.False(t, remote.Match(&internal.JobPosting{Location: "Austin, TX"}))

	// Onsite has no fallback on purpose: the absence of the word "remote" is not
	// evidence that an employer requires an office.
	test.False(t, onsite.Match(&internal.JobPosting{Location: "Austin, TX"}))

	test.False(t, remote.IsZero())
	test.True(t, internal.Filter{WorkplaceTypes: []internal.WorkplaceType{""}}.IsZero())
}

func TestFilterPostedSince(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	since := internal.Filter{PostedSince: cutoff}

	fresh := &internal.JobPosting{Title: "Engineer", PostedAt: cutoff.Add(24 * time.Hour)}
	stale := &internal.JobPosting{Title: "Engineer", PostedAt: cutoff.Add(-24 * time.Hour)}
	exactly := &internal.JobPosting{Title: "Engineer", PostedAt: cutoff}
	undated := &internal.JobPosting{Title: "Engineer"}

	test.True(t, since.Match(fresh))
	test.False(t, since.Match(stale))

	// The cutoff is inclusive, so a posting published exactly at it survives.
	test.True(t, since.Match(exactly))

	// Most boards publish no date at all. Treating those as recent would quietly
	// fill a "last week" query with postings of unknown age.
	test.False(t, since.Match(undated))

	// An update is not a publication: editing a description does not make a
	// nine-month-old requisition new.
	test.False(t, since.Match(&internal.JobPosting{Title: "Engineer", UpdatedAt: cutoff.Add(24 * time.Hour)}))

	// Comparison is by instant, not by wall-clock text, so a posting dated in
	// another zone compares correctly.
	elsewhere := time.FixedZone("UTC+9", 9*60*60)
	test.True(t, since.Match(&internal.JobPosting{
		Title:    "Engineer",
		PostedAt: cutoff.Add(time.Hour).In(elsewhere),
	}))

	test.False(t, since.IsZero())
}

func TestFilterApplyHonoursANewFieldAlone(t *testing.T) {
	t.Parallel()

	// End-to-end version of the IsZero trap: Apply short-circuits on IsZero, so a
	// filter carrying only one of the new fields has to actually filter rather
	// than pass the crawl through untouched.
	jobs := internal.Jobs(func(yield func(*internal.JobPosting, error) bool) {
		yield(&internal.JobPosting{Title: "Engineer", EmploymentType: internal.EmploymentTypeInternship}, nil)
		yield(&internal.JobPosting{Title: "Engineer", EmploymentType: internal.EmploymentTypeFullTime}, nil)
		yield(&internal.JobPosting{Title: "Engineer"}, nil)
	})

	filter := internal.Filter{EmploymentTypes: []internal.EmploymentType{internal.EmploymentTypeInternship}}

	var kept []internal.EmploymentType
	for job, err := range filter.Apply(jobs) {
		must.NoError(t, err)

		kept = append(kept, job.EmploymentType)
	}

	must.Eq(t, []internal.EmploymentType{internal.EmploymentTypeInternship}, kept)
}
