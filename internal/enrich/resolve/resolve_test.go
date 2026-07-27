package resolve_test

import (
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich/resolve"
	"github.com/shoenig/test/must"
)

// TestNormalizeNameCollapsesSpellings covers the transformation the whole join
// rests on: an ATS slug, a legal name and an all-caps government string must
// reduce to one comparable form.
func TestNormalizeNameCollapsesSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"Palo Alto Networks, Inc.", "palo alto networks"},
		{"PALO ALTO NETWORKS INC", "palo alto networks"},
		{"Cloudflare, Inc.", "cloudflare"},
		{"The Home Depot, Inc.", "home depot"},
		{"Anduril Industries, Inc.", "anduril industries"},
		{"Beta Robotics GmbH", "beta robotics"},
		{"Acme Holdings Group Ltd", "acme"},
		// Ampersands become a word rather than vanishing, so "AT&T" and "AT T"
		// stay different strings instead of both collapsing onto "att".
		{"AT&T Inc.", "at and t"},
		{"1Password", "1password"},
		// A name made only of legal forms would normalize to nothing and then
		// match every other such name, so the words are kept.
		{"The Company", "the company"},
		{"", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()

			must.Eq(t, tc.want, resolve.NormalizeName(tc.raw))
		})
	}
}

// TestSquashMatchesATSSlugs: most slugs are the legal name with the spaces taken
// out, which is the second matching rule.
func TestSquashMatchesATSSlugs(t *testing.T) {
	t.Parallel()

	must.Eq(t, resolve.Squash("Palo Alto Networks, Inc."), resolve.Squash("paloaltonetworks"))
	must.Eq(t, resolve.Squash("Cloudflare, Inc."), resolve.Squash("cloudflare"))
	must.NotEq(t, resolve.Squash("Stripe, Inc."), resolve.Squash("stripes"))
}

// filer builds an entity for the tests below.
func filer(cik, name, ticker string) resolve.Entity {
	return resolve.Entity{ID: cik, Name: name, Ticker: ticker}
}

// TestUniqueExactNameIsAccepted is the happy path, and the only shape that ever
// reaches the committed table automatically.
func TestUniqueExactNameIsAccepted(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{{Platform: "greenhouse", Key: "cloudflare", Company: "cloudflare"}},
		[]resolve.Entity{filer("0001477333", "Cloudflare, Inc.", "NET")},
	)

	must.Len(t, 1, result.Matches)
	must.SliceEmpty(t, result.Candidates)
	must.Eq(t, "0001477333", result.Matches[0].Entity.ID)
	must.Eq(t, enrich.ConfidenceHigh, result.Matches[0].Confidence)
	must.Eq(t, enrich.MethodEDGARExactName, result.Matches[0].Method)
}

// TestSlugOnlyMatchIsAccepted covers the platforms whose display name is a
// hostname or tenant URL, where the key is the only readable identifier.
func TestSlugOnlyMatchIsAccepted(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{{
			Platform: "workday",
			Key:      "paloaltonetworks",
			Company:  "paloaltonetworks.wd5.myworkdayjobs.com",
		}},
		[]resolve.Entity{filer("0001327567", "Palo Alto Networks Inc", "PANW")},
	)

	must.Len(t, 1, result.Matches)
	must.Eq(t, enrich.MethodEDGARExactKey, result.Matches[0].Method)
}

// TestAmbiguousEntityIsRefused is the test that protects users from a plausible
// wrong answer: two filers whose names normalize identically cannot both be the
// company, so neither is committed.
func TestAmbiguousEntityIsRefused(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{{Platform: "greenhouse", Key: "atlas", Company: "atlas"}},
		[]resolve.Entity{
			filer("0000000011", "Atlas Corp", "ATL"),
			filer("0000000022", "Atlas Inc.", "ATS"),
		},
	)

	must.SliceEmpty(t, result.Matches, must.Sprint("an ambiguous name must not be committed"))
	must.Len(t, 2, result.Candidates)

	for _, candidate := range result.Candidates {
		must.Eq(t, enrich.ConfidenceMedium, candidate.Confidence)
		must.StrContains(t, candidate.Why, "ambiguous")
	}
}

// TestOneEmployerOnTwoPlatformsIsAccepted is the case a naive uniqueness rule
// gets wrong. A company on Greenhouse and on Workday is still one company, and
// refusing both because they share a filer would throw away the matches most
// worth having.
func TestOneEmployerOnTwoPlatformsIsAccepted(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{
			{Platform: "greenhouse", Key: "cloudflare", Company: "Cloudflare"},
			{Platform: "ashbyhq", Key: "cloudflare", Company: "cloudflare"},
		},
		[]resolve.Entity{filer("0001477333", "Cloudflare, Inc.", "NET")},
	)

	must.Len(t, 2, result.Matches)
	must.SliceEmpty(t, result.Candidates)
}

// TestTwoCompaniesClaimingOneFilerAreRefused is the other half of the rule:
// sharing a filer is only fine when the display names agree.
func TestTwoCompaniesClaimingOneFilerAreRefused(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{
			{Platform: "greenhouse", Key: "acme", Company: "Acme"},
			{Platform: "lever", Key: "acme2", Company: "Acme Aerospace"},
		},
		[]resolve.Entity{filer("0000000033", "Acme Inc", "ACM")},
	)

	must.SliceEmpty(t, result.Matches)
	must.Len(t, 2, result.Candidates)

	for _, candidate := range result.Candidates {
		must.StrContains(t, candidate.Why, "contested")
	}
}

// TestTrailingDigitMatchesAreCandidatesOnly: an ATS appends a digit when a slug
// is taken, so "acme2" probably is Acme — but "2u" and "3m" are companies whose
// names are mostly digits, so the transformation is a hint for a reviewer rather
// than evidence.
func TestTrailingDigitMatchesAreCandidatesOnly(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{{Platform: "greenhouse", Key: "acme2", Company: "acme2"}},
		[]resolve.Entity{filer("0000000033", "Acme Inc", "ACM")},
	)

	must.SliceEmpty(t, result.Matches)
	must.Len(t, 1, result.Candidates)
	must.Eq(t, enrich.ConfidenceMedium, result.Candidates[0].Confidence)
	must.StrContains(t, result.Candidates[0].Why, "trailing digits")
}

// TestUnmatchedSourcesAreSilent: the overwhelmingly common outcome must not fill
// the review queue with 2,000 rows saying "EDGAR has never heard of this
// startup", because a queue nobody can read is a queue nobody reads.
func TestUnmatchedSourcesAreSilent(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{
			{Platform: "ashbyhq", Key: "somestartup", Company: "somestartup"},
			{Platform: "lever", Key: "anotherone", Company: "anotherone"},
		},
		[]resolve.Entity{filer("0001477333", "Cloudflare, Inc.", "NET")},
	)

	must.SliceEmpty(t, result.Matches)
	must.SliceEmpty(t, result.Candidates)
}

// TestShortKeysDoNotTriggerDigitTrimming guards the length floor on digit
// trimming. Without it, a slug like "ai2" would reduce to "ai" and propose every
// two-letter filer, which is a coincidence rather than a lead — and the "2u" and
// "3m" family shows that digits inside a company name are ordinary.
func TestShortKeysDoNotTriggerDigitTrimming(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{
			{Platform: "greenhouse", Key: "ai2", Company: "ai2"},
			{Platform: "greenhouse", Key: "2u", Company: "2u"},
		},
		[]resolve.Entity{
			filer("0000000044", "AI Inc", "AI"),
			filer("0000000055", "Two Corp", "TWO"),
		},
	)

	must.SliceEmpty(t, result.Matches)
	must.SliceEmpty(t, result.Candidates)
}
