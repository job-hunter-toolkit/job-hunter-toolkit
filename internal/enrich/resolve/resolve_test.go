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

// TestShortNameNeedsCorroboration is the rule the first live run bought.
//
// "MKS INC" and the MKS the board belongs to are unique in both directions and
// still different companies, which is exactly how ten wrong rows reached a
// reviewed table. A name this short is not evidence by itself.
func TestShortNameNeedsCorroboration(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{{Platform: "teamtailor", Key: "mks", Company: "mks"}},
		[]resolve.Entity{filer("0000000066", "MKS INC", "MKSI")},
	)

	must.SliceEmpty(t, result.Matches, must.Sprint(
		"a three-letter name with nothing corroborating it must not be committed"))
	must.Len(t, 1, result.Candidates)
	must.Eq(t, enrich.ConfidenceMedium, result.Candidates[0].Confidence)
	must.StrContains(t, result.Candidates[0].Why, "uncorroborated short name")
}

// TestShortNameWithMatchingTickerIsAccepted: RH trades as RH, and an exchange
// symbol is an identifier the entity was given rather than a word anybody can
// be spelled with.
func TestShortNameWithMatchingTickerIsAccepted(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{{Platform: "oraclecloud", Key: "rh", Company: "rh"}},
		[]resolve.Entity{filer("0001528849", "RH", "RH")},
	)

	must.Len(t, 1, result.Matches)
	must.SliceEmpty(t, result.Candidates)
	must.Eq(t, enrich.ConfidenceHigh, result.Matches[0].Confidence)
}

// TestShortNameWithMatchingWebsiteIsAccepted covers the other corroborating
// identifier: a registered domain, joined to the filer by CIK rather than by
// name.
func TestShortNameWithMatchingWebsiteIsAccepted(t *testing.T) {
	t.Parallel()

	entity := filer("0000000077", "CAE INC", "NYSE:CAE")
	entity.Websites = []string{"https://www.cae.com/careers/"}

	result := resolve.Sources(
		[]resolve.Source{{Platform: "workday", Key: "cae", Company: "cae"}},
		[]resolve.Entity{entity},
	)

	must.Len(t, 1, result.Matches)
	must.SliceEmpty(t, result.Candidates)
}

// TestCorroborationIsRejectedByADifferentDomain is the measured case: Post
// Holdings publishes postholdings.com, NMI Holdings nationalmi.com, and neither
// domain says the board named "post" or "nmi" is that filer.
func TestCorroborationIsRejectedByADifferentDomain(t *testing.T) {
	t.Parallel()

	entity := filer("0000000088", "NMI Holdings, Inc.", "NMIH")
	entity.Websites = []string{"https://www.nationalmi.com/"}

	_, ok := resolve.Corroborated(entity, resolve.NormalizeName(entity.Name))
	must.False(t, ok)

	result := resolve.Sources(
		[]resolve.Source{{Platform: "greenhouse", Key: "nmi", Company: "nmi"}},
		[]resolve.Entity{entity},
	)

	must.SliceEmpty(t, result.Matches)
	must.Len(t, 1, result.Candidates)
}

// TestDistinctiveNamesStillMatchWithoutCorroboration: the gate must not cost
// the matches the table exists for. A multi-word name, or one long enough to be
// a word somebody chose, is evidence on its own.
func TestDistinctiveNamesStillMatchWithoutCorroboration(t *testing.T) {
	t.Parallel()

	result := resolve.Sources(
		[]resolve.Source{
			{Platform: "greenhouse", Key: "beamtherapeutics", Company: "beamtherapeutics"},
			{Platform: "greenhouse", Key: "asana", Company: "asana"},
		},
		[]resolve.Entity{
			filer("0001745999", "Beam Therapeutics Inc.", "BEAM"),
			filer("0001477720", "Asana, Inc.", "ASAN"),
		},
	)

	must.Len(t, 2, result.Matches)
	must.SliceEmpty(t, result.Candidates)
}

// TestDistinctive states the threshold as a table, because it is the one number
// in this package chosen from a measurement rather than from a principle.
func TestDistinctive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		want bool
	}{
		{"palo alto networks", true},
		{"beam therapeutics", true},
		{"asana", true},
		{"block", true},
		{"post", false},
		{"glow", false},
		{"nmi", false},
		{"wf", false},
		{"", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			must.Eq(t, tc.want, resolve.Distinctive(tc.name))
		})
	}
}

// TestCorroboratedReadsHostsInEveryShapeTheyArriveIn: P856 values are typed in
// by hand, so a bare hostname, a full URL with a path and a brand top-level
// domain all have to reduce to the same answer.
func TestCorroboratedReadsHostsInEveryShapeTheyArriveIn(t *testing.T) {
	t.Parallel()

	for _, site := range []string{
		"cae.com",
		"http://cae.com",
		"https://www.cae.com/",
		"https://careers.cae.com/en/jobs?x=1",
		"https://www.cae.com:8443/",
		"https://global.cae/",
	} {
		t.Run(site, func(t *testing.T) {
			t.Parallel()

			entity := resolve.Entity{ID: "0000000077", Name: "CAE INC", Websites: []string{site}}

			evidence, ok := resolve.Corroborated(entity, "cae")
			must.True(t, ok, must.Sprintf("%s should corroborate", site))
			must.StrContains(t, evidence, "website")
		})
	}

	// A generic label is not an identity: every company on the web has a "www".
	entity := resolve.Entity{ID: "0000000099", Name: "WWW Inc", Websites: []string{"https://www.example.org/"}}
	_, ok := resolve.Corroborated(entity, "www")
	must.False(t, ok)
}
