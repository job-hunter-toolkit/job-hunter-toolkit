package internal_test

import (
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// greenhousePayWidget is the markup Greenhouse renders when an employer fills in
// its structured pay-range field. Taken from a live Databricks posting.
const greenhousePayWidget = `<div class="pay-range-container">` +
	`<div class="title">Zone 1 Pay Range</div>` +
	`<div class="pay-range"><span>$145,700</span>` +
	`<span class="divider">&mdash;</span><span>$200,300 USD</span></div></div>`

func TestParseCompensationFromDescriptionPrefersStructuredMarkup(t *testing.T) {
	t.Parallel()

	got := internal.ParseCompensationFromDescription(greenhousePayWidget)
	if got == nil {
		t.Fatal("ParseCompensationFromDescription() = nil, want the widget range")
	}

	if got.Min != 145700 || got.Max != 200300 {
		t.Errorf("range = %v-%v, want 145700-200300", got.Min, got.Max)
	}

	// A container that declares its contents to be the pay range is structural
	// evidence, not a guess, and must be reported as such.
	if got.Provenance != internal.ProvenanceStructured {
		t.Errorf("Provenance = %q, want %q", got.Provenance, internal.ProvenanceStructured)
	}
}

func TestParseCompensationFromDescriptionHandlesEncodedMarkup(t *testing.T) {
	t.Parallel()

	// Some boards publish their markup entity-encoded. A stripper that only
	// understands literal angle brackets leaves escaped tags between the two ends
	// of the range, which previously made a real range read as a lone lower bound.
	encoded := `&lt;div class=&quot;pay-range&quot;&gt;&lt;span&gt;$145,700&lt;/span&gt;` +
		`&lt;span class=&quot;divider&quot;&gt;&amp;mdash;&lt;/span&gt;` +
		`&lt;span&gt;$200,300 USD&lt;/span&gt;&lt;/div&gt;`

	got := internal.ParseCompensationFromDescription(encoded)
	if got == nil {
		t.Fatal("ParseCompensationFromDescription() = nil, want a range from encoded markup")
	}

	if got.Min != 145700 || got.Max != 200300 {
		t.Errorf("range = %v-%v, want 145700-200300", got.Min, got.Max)
	}
}

func TestParseCompensationFromDescriptionStructuredBeatsProse(t *testing.T) {
	t.Parallel()

	// When both are present the structural container wins, even though the prose
	// figure appears first in the document.
	description := `<p>The salary range for this position is $100,000 - $120,000.</p>` +
		greenhousePayWidget

	got := internal.ParseCompensationFromDescription(description)
	if got == nil {
		t.Fatal("ParseCompensationFromDescription() = nil, want a range")
	}

	if got.Provenance != internal.ProvenanceStructured {
		t.Fatalf("Provenance = %q, want the structural source to win", got.Provenance)
	}

	if got.Min != 145700 {
		t.Errorf("Min = %v, want the widget value 145700", got.Min)
	}
}

func TestParseCompensationFromDescriptionFallsBackToProse(t *testing.T) {
	t.Parallel()

	// No widget present, so prose is the only source; and it must be labelled
	// as the weaker one.
	got := internal.ParseCompensationFromDescription(
		`<p>The base salary range for this role is $185,000 - $235,000 per year.</p>`)
	if got == nil {
		t.Fatal("ParseCompensationFromDescription() = nil, want the prose range")
	}

	if got.Provenance != internal.ProvenanceDescription {
		t.Errorf("Provenance = %q, want %q", got.Provenance, internal.ProvenanceDescription)
	}

	if got.Min != 185000 || got.Max != 235000 {
		t.Errorf("range = %v-%v, want 185000-235000", got.Min, got.Max)
	}
}

func TestParseCompensationFromDescriptionIgnoresSimilarClassNames(t *testing.T) {
	t.Parallel()

	// Class matching is on whole tokens, so a differently-named container does
	// not get treated as the pay range widget. Here the only figure is a 401(k)
	// match, which prose parsing must also reject; so the answer is nil.
	description := `<div class="pay-range-note">401(k) match up to $6,000</div>`

	if got := internal.ParseCompensationFromDescription(description); got != nil {
		t.Errorf("ParseCompensationFromDescription() = %+v, want nil", got)
	}
}

func TestParseCompensationFromDescriptionEmpty(t *testing.T) {
	t.Parallel()

	if got := internal.ParseCompensationFromDescription(""); got != nil {
		t.Errorf("ParseCompensationFromDescription(\"\") = %+v, want nil", got)
	}
}

func TestParseCompensationFromDescriptionRejectsImplausibleWidgetValues(t *testing.T) {
	t.Parallel()

	// Structural provenance is not a licence to emit nonsense: plausibility is
	// still enforced, so a malformed widget does not become a salary.
	description := `<div class="pay-range"><span>$3</span><span>&mdash;</span><span>$7</span></div>`

	if got := internal.ParseCompensationFromDescription(description); got != nil {
		t.Errorf("ParseCompensationFromDescription() = %+v, want nil for implausible values", got)
	}
}

// TestParseCompensationFromDescriptionWidgetCurrency checks that the structured
// path reads the currency out of the widget rather than assuming.
//
// A container declaring itself the pay range is the strongest non-API evidence
// this toolkit has, which makes a wrong currency label here worse than anywhere
// else: it is reported as ProvenanceStructured, so a consumer weighing
// provenance trusts it more than prose.
func TestParseCompensationFromDescriptionWidgetCurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		description  string
		wantCurrency string
		wantMin      float64
		wantMax      float64
	}{
		{
			// The prefixed second operand used to break the range match outright,
			// so the widget reported C$95,000 as a lone USD figure.
			name: "canadian widget keeps both ends and its currency",
			description: `<div class="pay-range"><span>C$95,000</span>` +
				`<span class="divider">&mdash;</span><span>C$120,000</span></div>`,
			wantCurrency: "CAD",
			wantMin:      95000,
			wantMax:      120000,
		},
		{
			name: "code written after the range",
			description: `<div class="pay-range"><span>$95,000</span>` +
				`<span class="divider">&mdash;</span><span>$120,000 CAD</span></div>`,
			wantCurrency: "CAD",
			wantMin:      95000,
			wantMax:      120000,
		},
		{
			// The live Databricks widget, which really does say USD.
			name:         "usd widget",
			description:  greenhousePayWidget,
			wantCurrency: "USD",
			wantMin:      145700,
			wantMax:      200300,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := internal.ParseCompensationFromDescription(tt.description)
			must.NotNil(t, got, must.Sprint("ParseCompensationFromDescription() = nil"))

			test.Eq(t, internal.ProvenanceStructured, got.Provenance)
			test.Eq(t, tt.wantCurrency, got.Currency)
			test.Eq(t, tt.wantMin, got.Min)
			test.Eq(t, tt.wantMax, got.Max)
		})
	}
}

func TestParseCompensationFromDescriptionWidgetStatesACeiling(t *testing.T) {
	t.Parallel()

	// A widget holding one open-ended figure states a maximum. Recording it as
	// Min inverted the meaning, and the CSV writer then emitted pay_min=200000
	// with an empty pay_max.
	got := internal.ParseCompensationFromDescription(
		`<div class="pay-range"><span>Up to $200,000 USD</span></div>`)
	must.NotNil(t, got, must.Sprint("ParseCompensationFromDescription() = nil"))

	test.Eq(t, 0.0, got.Min)
	test.Eq(t, 200000.0, got.Max)
}

func TestParseCompensationFromDescriptionWidgetHandlesNonASCII(t *testing.T) {
	t.Parallel()

	// The widget's own text used to be matched in one string and sliced in
	// another. strings.ToLower shrinks U+0130 (İ) from two bytes to one, so a
	// non-ASCII label inside the container shifted the period window off "per
	// year" and, with enough of them, made the slice panic outright.
	for _, repeats := range []int{0, 16, 18, 25, 40} {
		description := `<div class="pay-range"><span>` +
			strings.Repeat("İstanbul İK İş İlanı ", repeats) +
			`$150,000</span><span class="divider">&mdash;</span>` +
			`<span>$200,000 per year</span></div>`

		got := internal.ParseCompensationFromDescription(description)
		must.NotNil(t, got, must.Sprintf("nil with %d label repetitions", repeats))

		test.Eq(t, 150000.0, got.Min, test.Sprintf("%d repetitions", repeats))
		test.Eq(t, 200000.0, got.Max, test.Sprintf("%d repetitions", repeats))
		test.Eq(t, internal.PeriodYear, got.Period, test.Sprintf("%d repetitions", repeats))
	}
}

func TestCompensationMoreTrustedThan(t *testing.T) {
	t.Parallel()

	employer := &internal.Compensation{Max: 1, Provenance: internal.ProvenanceEmployer}
	structured := &internal.Compensation{Max: 1, Provenance: internal.ProvenanceStructured}
	prose := &internal.Compensation{Max: 1, Provenance: internal.ProvenanceDescription}

	if !employer.MoreTrustedThan(structured) {
		t.Error("employer should outrank structured")
	}

	if !structured.MoreTrustedThan(prose) {
		t.Error("structured should outrank prose")
	}

	if prose.MoreTrustedThan(structured) {
		t.Error("prose should not outrank structured")
	}

	if !prose.MoreTrustedThan(nil) {
		t.Error("any provenance should outrank nothing")
	}

	var nilComp *internal.Compensation
	if nilComp.MoreTrustedThan(prose) {
		t.Error("nil should not outrank a real value")
	}
}
