package internal_test

import (
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
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
