package internal_test

import (
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
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
