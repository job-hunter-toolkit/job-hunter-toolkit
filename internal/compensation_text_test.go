package internal_test

import (
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestParseCompensationFromTextAccepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		wantMin float64
		wantMax float64
		period  internal.Period
	}{
		{
			name:    "explicit annual range",
			text:    "The salary range for this position is $150,000 - $200,000 per year.",
			wantMin: 150000,
			wantMax: 200000,
		},
		{
			name:    "K suffix with en dash",
			text:    "Base pay range: $180K–$220K",
			wantMin: 180000,
			wantMax: 220000,
		},
		{
			name:    "hourly rate range",
			text:    "The hourly rate for this role is $22.50 to $28.00 per hour.",
			wantMin: 22.50,
			wantMax: 28.00,
			period:  internal.PeriodHour,
		},
		{
			name:    "single figure with starting at",
			text:    "Compensation for this role is starting at $135,000 annually.",
			wantMin: 135000,
		},
		{
			name:    "pay transparency preamble",
			text:    "Pay transparency: the expected pay for this position is $95,000 — $115,000.",
			wantMin: 95000,
			wantMax: 115000,
		},
		{
			name:    "html markup is stripped",
			text:    "<p><strong>Salary Range:</strong> $120,000 &ndash; $145,000</p>",
			wantMin: 120000,
			wantMax: 145000,
		},
		{
			name:    "usd prefix instead of symbol",
			text:    "Annual compensation: USD 130,000 - USD 160,000",
			wantMin: 130000,
			wantMax: 160000,
		},
		{
			name:    "unlabelled small figures read as hourly",
			text:    "This position pays $18.00 - $24.00.",
			wantMin: 18,
			wantMax: 24,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := internal.ParseCompensationFromText(tt.text)
			if got == nil {
				t.Fatalf("ParseCompensationFromText() = nil, want a range for %q", tt.text)
			}

			if got.Min != tt.wantMin {
				t.Errorf("Min = %v, want %v", got.Min, tt.wantMin)
			}

			if tt.wantMax != 0 && got.Max != tt.wantMax {
				t.Errorf("Max = %v, want %v", got.Max, tt.wantMax)
			}

			if tt.period != internal.PeriodUnknown && got.Period != tt.period {
				t.Errorf("Period = %q, want %q", got.Period, tt.period)
			}

			// Anything read out of prose must be marked as such, so it is never
			// mistaken for a figure the employer published in a real field.
			if got.Provenance != internal.ProvenanceDescription {
				t.Errorf("Provenance = %q, want %q", got.Provenance, internal.ProvenanceDescription)
			}
		})
	}
}

// TestParseCompensationFromTextRejects is the important half of this parser.
//
// Job descriptions are full of dollar amounts that are not wages. Reporting one
// of those as a salary is worse than reporting nothing: it silently corrupts
// every pay filter and comparison built on top of it. Each case below is drawn
// from language that really appears in postings.
func TestParseCompensationFromTextRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
	}{
		{
			name: "empty",
			text: "",
		},
		{
			name: "no money at all",
			text: "We are looking for a Senior Security Engineer to join our team.",
		},
		{
			name: "money with no pay cue",
			text: "Our customers process $4,500,000 in transactions every day.",
		},
		{
			name: "401k match",
			text: "Benefits include a 401(k) with company match up to $5,000.",
		},
		{
			name: "tuition reimbursement",
			text: "We offer tuition reimbursement of up to $5,250 per year.",
		},
		{
			name: "funding raised",
			text: "We have raised $150,000,000 in Series C funding.",
		},
		{
			name: "annual recurring revenue",
			text: "The company crossed $100M ARR last quarter.",
		},
		{
			name: "signing bonus near a salary cue",
			text: "Salary is competitive. We also offer a signing bonus of $20,000.",
		},
		{
			name: "insurance deductible",
			text: "Our health plan has a $500 deductible and a low monthly premium.",
		},
		{
			name: "equity value",
			text: "Compensation includes equity worth up to $250,000 over four years.",
		},
		{
			name: "figure too small to be a wage",
			text: "The salary includes a $50 monthly wellness credit.",
		},
		{
			name: "figure too large to be a wage",
			text: "The salary budget for the department is $75,000,000.",
		},
		{
			name: "implausibly wide range is two unrelated numbers",
			text: "Salary and benefits: $30,000 - $9,000,000",
		},
		{
			name: "stipend",
			text: "Interns receive a housing stipend of $3,000 per month.",
		},
		{
			name: "referral bonus",
			text: "Employees earn a referral bonus of $2,500 per hire.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := internal.ParseCompensationFromText(tt.text); got != nil {
				t.Errorf("ParseCompensationFromText(%q) = %+v, want nil", tt.text, got)
			}
		})
	}
}

func TestParseCompensationFromTextPrefersTheSalaryOverOtherAmounts(t *testing.T) {
	t.Parallel()

	// A realistic description mentioning several amounts. The salary range must
	// win over the 401(k) match and the signing bonus.
	text := `
		<h3>About the role</h3>
		<p>We raised $200M in Series D funding last year and serve customers
		processing $12B annually.</p>
		<p>The base salary range for this position is $185,000 - $235,000 per year,
		depending on experience and location.</p>
		<ul>
			<li>401(k) with match up to $6,000</li>
			<li>Tuition reimbursement up to $5,250</li>
			<li>Signing bonus of $15,000</li>
		</ul>
	`

	got := internal.ParseCompensationFromText(text)
	if got == nil {
		t.Fatal("ParseCompensationFromText() = nil, want the salary range")
	}

	if got.Min != 185000 || got.Max != 235000 {
		t.Errorf("range = %v-%v, want 185000-235000", got.Min, got.Max)
	}
}

func TestParseCompensationFromTextAnnualizesForFiltering(t *testing.T) {
	t.Parallel()

	// An hourly range read from prose must still annualize, so a single pay
	// filter works across hourly and salaried postings.
	got := internal.ParseCompensationFromText("Pay rate: $30.00 - $35.00 per hour")
	if got == nil {
		t.Fatal("ParseCompensationFromText() = nil, want an hourly range")
	}

	if got.Period != internal.PeriodHour {
		t.Fatalf("Period = %q, want %q", got.Period, internal.PeriodHour)
	}

	annual, ok := got.AnnualMax()
	if !ok {
		t.Fatal("AnnualMax() reported no value")
	}

	if want := 35.0 * 2080; annual != want {
		t.Errorf("AnnualMax() = %v, want %v", annual, want)
	}
}

// TestParseCompensationFromTextMagnitudeSuffixNeedsAWordBoundary pins the fix
// for a suffix group that used to run past the end of the figure.
//
// The optional ([km]) magnitude group was unanchored, so it consumed the first
// letter of the *following* word and parseMoney multiplied by 1e3 or 1e6. Two
// distinct failures came out of that, and both are represented here:
//
//   - the inflated figure blew past the plausibility ceiling and the whole
//     posting silently reported no pay, destroying valid ranges;
//   - for a small figure the inflated value stayed plausible and was published,
//     which invents a salary that appears nowhere in the posting. That is the
//     failure this parser exists to prevent.
func TestParseCompensationFromTextMagnitudeSuffixNeedsAWordBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		text       string
		wantMin    float64
		wantMax    float64
		wantPeriod internal.Period
		wantAnnual float64
	}{
		{
			// The 'm' of "monthly" used to be read as the mega suffix, making
			// this $12,000,000 and pushing it past maxPlausibleAnnual.
			name:       "monthly does not become a mega suffix",
			text:       "Salary: $12,000 monthly",
			wantMin:    12000,
			wantPeriod: internal.PeriodMonth,
			wantAnnual: 144000,
		},
		{
			// An entire explicit range was lost to the 'm' of "minimum".
			name:       "minimum after a range keeps both ends",
			text:       "The pay range for this role is $140,000 to $180,000 minimum.",
			wantMin:    140000,
			wantMax:    180000,
			wantAnnual: 180000,
		},
		{
			name:       "maximum after a lone figure",
			text:       "The salary for this role is $95,000 maximum.",
			wantMax:    95000,
			wantAnnual: 95000,
		},
		{
			name:       "minimum after a lone figure",
			text:       "Base pay of $85,000 minimum for this role.",
			wantMin:    85000,
			wantAnnual: 85000,
		},
		{
			// The legitimate forms the suffix exists for must keep working.
			name:       "k suffix",
			text:       "Base pay range: $120k",
			wantMin:    120000,
			wantAnnual: 120000,
		},
		{
			name:       "m suffix",
			text:       "Salary range: $1.2m",
			wantMin:    1_200_000,
			wantAnnual: 1_200_000,
		},
		{
			// Spelled out. The old unanchored group handled this correctly by
			// accident, by swallowing the leading 'm' of "million"; anchoring the
			// suffix would have turned it into $1.20 without an explicit case.
			name:       "million spelled out",
			text:       "Salary: $1.2 million",
			wantMin:    1_200_000,
			wantAnnual: 1_200_000,
		},
		{
			name:       "k suffix on both ends of a range",
			text:       "Base pay range: $120K-$150K",
			wantMin:    120000,
			wantMax:    150000,
			wantAnnual: 150000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := internal.ParseCompensationFromText(tt.text)
			must.NotNil(t, got, must.Sprintf("ParseCompensationFromText(%q) = nil", tt.text))

			test.Eq(t, tt.wantMin, got.Min)
			test.Eq(t, tt.wantMax, got.Max)

			if tt.wantPeriod != internal.PeriodUnknown {
				test.Eq(t, tt.wantPeriod, got.Period)
			}

			annual, ok := got.AnnualMax()
			must.True(t, ok, must.Sprint("AnnualMax() reported no value"))
			test.Eq(t, tt.wantAnnual, annual)
		})
	}
}

// TestParseCompensationFromTextDoesNotFabricateAMagnitude is the dangerous half
// of the suffix bug, kept separate because the assertion is about a number that
// must never appear rather than one that must.
//
// "The salary is $45 knowing the market." used to publish min=45,000: the 'k' of
// "knowing" became a kilo suffix, and the result was plausible enough to survive
// every guard the parser has. Nothing in that sentence says 45,000.
func TestParseCompensationFromTextDoesNotFabricateAMagnitude(t *testing.T) {
	t.Parallel()

	got := internal.ParseCompensationFromText("The salary is $45 knowing the market.")
	must.NotNil(t, got, must.Sprint("expected the literal $45 to still be read"))

	// The only figure written is 45. It carries no period, so the documented
	// magnitude heuristic reads it as an hourly rate; what matters is that the
	// parser reports a number the posting actually contains.
	test.Eq(t, 45.0, got.Min)
	test.Eq(t, 0.0, got.Max)
	test.NotEq(t, 45000.0, got.Min)
}

// TestParseCompensationFromTextDayRates covers a period the parser could reach
// from an API but never from prose.
//
// periodFromContext had no day branch, so a day rate came back PeriodUnknown,
// effectivePeriod's hourly magnitude ceiling claimed anything at or under 250,
// and the figure was annualized at 2080 instead of 260. A $200/day contract was
// published as a $416,000 job — 8x wrong, and carrying the same provenance as a
// correct figure. Day rates above the ceiling failed the other way: read as
// annual, then rejected as implausibly low, so the posting reported no pay.
func TestParseCompensationFromTextDayRates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		text       string
		wantMin    float64
		wantMax    float64
		wantAnnual float64
	}{
		{
			name:       "per day",
			text:       "This role pays $200 per day.",
			wantMin:    200,
			wantAnnual: 52000,
		},
		{
			name:       "day range",
			text:       "The pay rate for this role is $200 - $240 per day.",
			wantMin:    200,
			wantMax:    240,
			wantAnnual: 62400,
		},
		{
			// Above hourlyMagnitudeCeiling, so this used to be read as annual and
			// then thrown out as implausibly low: no pay reported at all.
			name:       "slash day above the hourly ceiling",
			text:       "The pay rate for this role is $600/day.",
			wantMin:    600,
			wantAnnual: 156000,
		},
		{
			name:       "per diem",
			text:       "The pay rate for this role is $450 per diem.",
			wantMin:    450,
			wantAnnual: 117000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := internal.ParseCompensationFromText(tt.text)
			must.NotNil(t, got, must.Sprintf("ParseCompensationFromText(%q) = nil", tt.text))

			test.Eq(t, internal.PeriodDay, got.Period)
			test.Eq(t, tt.wantMin, got.Min)
			test.Eq(t, tt.wantMax, got.Max)

			annual, ok := got.AnnualMax()
			must.True(t, ok, must.Sprint("AnnualMax() reported no value"))
			test.Eq(t, tt.wantAnnual, annual)

			// The specific wrong answer this test exists for.
			test.NotEq(t, max(tt.wantMin, tt.wantMax)*2080, annual)
		})
	}
}

// TestParseCompensationFromTextCurrency covers dollar signs that are not US
// dollars.
//
// Two defects compounded here. Currency was hardcoded USD for anything the "$"
// pattern matched, and the range pattern accepted only "$" or nothing in front
// of its second operand, so "C$120,000" broke the range match and the text fell
// through to the lone-figure path. "C$95,000 - C$120,000" was therefore
// published as USD 95,000 with no upper bound: wrong currency and a discarded
// ceiling, in a field nothing downstream converts.
func TestParseCompensationFromTextCurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		text         string
		wantCurrency string
		wantMin      float64
		wantMax      float64
	}{
		{
			name:         "canadian dollars keep both ends",
			text:         "The salary range for this position is C$95,000 - C$120,000.",
			wantCurrency: "CAD",
			wantMin:      95000,
			wantMax:      120000,
		},
		{
			name:         "australian dollars",
			text:         "Salary range: A$110,000 - A$140,000 per year",
			wantCurrency: "AUD",
			wantMin:      110000,
			wantMax:      140000,
		},
		{
			name:         "two letter country prefix",
			text:         "Salary range: CA$95,000 - CA$120,000",
			wantCurrency: "CAD",
			wantMin:      95000,
			wantMax:      120000,
		},
		{
			// The form boards actually use most: bare dollar signs with the code
			// written after the range.
			name:         "code written after the range",
			text:         "The salary range is $95,000 - $120,000 CAD.",
			wantCurrency: "CAD",
			wantMin:      95000,
			wantMax:      120000,
		},
		{
			name:         "explicit usd on both ends",
			text:         "Annual compensation: USD 130,000 - USD 160,000",
			wantCurrency: "USD",
			wantMin:      130000,
			wantMax:      160000,
		},
		{
			// An unmarked "$" is recorded as USD. That is an assumption, and
			// docs/compensation.md says so; it is pinned here so a change to it
			// is a deliberate one.
			name:         "unmarked dollars default to usd",
			text:         "The salary range for this position is $150,000 - $200,000 per year.",
			wantCurrency: "USD",
			wantMin:      150000,
			wantMax:      200000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := internal.ParseCompensationFromText(tt.text)
			must.NotNil(t, got, must.Sprintf("ParseCompensationFromText(%q) = nil", tt.text))

			test.Eq(t, tt.wantCurrency, got.Currency)
			test.Eq(t, tt.wantMin, got.Min)
			test.Eq(t, tt.wantMax, got.Max)
		})
	}
}

// TestParseCompensationFromTextRefusesMixedCurrencyRanges records a deliberate
// choice: two figures in two currencies are not a range.
//
// Nothing in this toolkit converts currencies, so there is no span between
// C$95,000 and A$120,000 that could be reported honestly. Picking one of the two
// codes would assert something the posting does not say, so the match is refused
// outright and neither end is salvaged as a lone figure — the same rule already
// applied to a range rejected as two unrelated numbers.
func TestParseCompensationFromTextRefusesMixedCurrencyRanges(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"The salary range is C$95,000 - A$120,000.",
		"The salary range is CAD 95,000 - USD 120,000.",
	} {
		test.Nil(t, internal.ParseCompensationFromText(text),
			test.Sprintf("ParseCompensationFromText(%q) should refuse a mixed-currency range", text))
	}
}

// TestParseCompensationFromTextOpenEndedBounds checks which end of the range a
// lone figure lands on.
//
// "Up to $200,000" was recorded as Min=200,000 with Max unset, which inverts the
// sentence: the CSV writer emitted pay_min=200000 with an empty pay_max, and
// anything reading the JSON "min" field was told a stated ceiling was a floor.
func TestParseCompensationFromTextOpenEndedBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		wantMin float64
		wantMax float64
	}{
		{
			name:    "up to sets the maximum",
			text:    "Salary up to $200,000 depending on experience.",
			wantMax: 200000,
		},
		{
			name:    "maximum of sets the maximum",
			text:    "Salary: a maximum of $180,000 per year.",
			wantMax: 180000,
		},
		{
			name:    "as much as sets the maximum",
			text:    "The salary for this role is as much as $175,000.",
			wantMax: 175000,
		},
		{
			// The symmetric cases: these already behaved, and must keep doing so.
			name:    "starting at sets the minimum",
			text:    "Compensation for this role is starting at $135,000 annually.",
			wantMin: 135000,
		},
		{
			name:    "from sets the minimum",
			text:    "The salary for this role starts from $120,000.",
			wantMin: 120000,
		},
		{
			name:    "at least sets the minimum",
			text:    "Base pay for this role is at least $115,000.",
			wantMin: 115000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := internal.ParseCompensationFromText(tt.text)
			must.NotNil(t, got, must.Sprintf("ParseCompensationFromText(%q) = nil", tt.text))

			test.Eq(t, tt.wantMin, got.Min)
			test.Eq(t, tt.wantMax, got.Max)

			// Whichever end it landed on, the figure must still annualize, so
			// --min-pay keeps working on an open-ended posting.
			annual, ok := got.AnnualMax()
			must.True(t, ok, must.Sprint("AnnualMax() reported no value"))
			test.Eq(t, max(tt.wantMin, tt.wantMax), annual)
		})
	}
}

// turkishPreamble is prose whose uppercase letters do not survive lowercasing at
// the same byte length: U+0130 (İ) shrinks from two bytes to one.
//
// Cue and period windows used to slice the lowercased description with offsets
// measured against the original, so every İ before a pay figure shifted those
// windows one byte to the right. Measured on the range below: 61 İ slid the
// period window off "per year" and the posting was published with no period at
// all, and 70 made the window slice panic with "slice bounds out of range".
// A Turkish-language posting reaches either count easily.
const turkishPreamble = "İstanbul İK İş İlanı: "

func TestParseCompensationFromTextNonASCIIDoesNotShiftWindows(t *testing.T) {
	t.Parallel()

	// Each repetition contributes four İ, so this spans the drift threshold, the
	// panic threshold, and well past both.
	for _, repeats := range []int{0, 16, 18, 25, 40, 60} {
		text := strings.Repeat(turkishPreamble, repeats) +
			"The salary range for this position is $150,000 - $200,000 per year."

		got := internal.ParseCompensationFromText(text)
		must.NotNil(t, got, must.Sprintf("nil with %d preamble repetitions", repeats))

		test.Eq(t, 150000.0, got.Min, test.Sprintf("%d repetitions", repeats))
		test.Eq(t, 200000.0, got.Max, test.Sprintf("%d repetitions", repeats))

		// The period is the window that drifts first, so it is the sensitive
		// assertion; the cue window fails later and turns the whole range to nil.
		test.Eq(t, internal.PeriodYear, got.Period, test.Sprintf("%d repetitions", repeats))
	}
}

// TestCompensationAnnualizationIsAppliedOnceAtTheSameRate guards the constant
// --min-pay depends on.
//
// Annualizing is computed on demand from Min/Max and never written back, so the
// multiplier cannot compound; this pins that, since a stored annual figure that
// got annualized a second time would read as $77M/yr and clear every filter.
func TestCompensationAnnualizationIsAppliedOnceAtTheSameRate(t *testing.T) {
	t.Parallel()

	hourly := internal.ParseCompensationFromText("Pay rate: $30.00 - $37.00 per hour")
	must.NotNil(t, hourly, must.Sprint("expected an hourly range"))

	first, ok := hourly.AnnualMax()
	must.True(t, ok, must.Sprint("AnnualMax() reported no value"))
	test.Eq(t, 37.0*2080, first)

	second, ok := hourly.AnnualMax()
	must.True(t, ok, must.Sprint("AnnualMax() reported no value on the second call"))
	test.Eq(t, first, second)

	// The stored figures stay in the period the posting stated.
	test.Eq(t, 30.0, hourly.Min)
	test.Eq(t, 37.0, hourly.Max)

	bottom, ok := hourly.AnnualMin()
	must.True(t, ok, must.Sprint("AnnualMin() reported no value"))
	test.Eq(t, 30.0*2080, bottom)

	// The same 2080 applies to a lone hourly figure and to an inferred one, so a
	// single --min-pay threshold means the same thing across all three shapes.
	inferred := internal.ParseCompensationFromText("This position pays $24.00.")
	must.NotNil(t, inferred, must.Sprint("expected an inferred hourly figure"))

	inferredAnnual, ok := inferred.AnnualMax()
	must.True(t, ok, must.Sprint("AnnualMax() reported no value"))
	test.Eq(t, 24.0*2080, inferredAnnual)
}

func TestCompensationProvenanceIsDistinguishable(t *testing.T) {
	t.Parallel()

	// The whole point of the field: a consumer can tell an employer-published
	// range from one inferred out of prose, and weigh them differently.
	fromProse := internal.ParseCompensationFromText("Salary range: $100,000 - $120,000")
	if fromProse == nil {
		t.Fatal("expected a parsed range")
	}

	employer := &internal.Compensation{
		Min: 100000, Max: 120000, Provenance: internal.ProvenanceEmployer,
	}

	if fromProse.Provenance == employer.Provenance {
		t.Error("prose and employer provenance are indistinguishable")
	}
}
