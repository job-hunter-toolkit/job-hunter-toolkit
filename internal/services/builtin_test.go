package services

import (
	"strings"
	"testing"
)

func TestSourcesSeparateKeyFromDisplayName(t *testing.T) {
	t.Parallel()

	// Every registered source must carry both identities. Conflating them put raw
	// tenant URLs into the user-facing company list and made `--company <URL>`
	// silently match nothing.
	for _, source := range Builtin {
		if source.Key == "" {
			t.Errorf("source %+v has no ATS key", source)
		}

		if source.Company == "" {
			t.Errorf("source with key %q has no display name", source.Key)
		}

		if source.Jobs == nil {
			t.Errorf("source %q has no fetch function", source.Key)
		}
	}
}

func TestCompaniesAreNamesNotURLs(t *testing.T) {
	t.Parallel()

	// The company list is user-facing and is meant to be readable and diffable.
	// Workday keys are tenant URLs and Phenom keys are hostnames; leaking those
	// here made ~110 entries sort under "https://" instead of alphabetically.
	//
	// Note this does not forbid a dot: Ashby permits domain-style board names, so
	// "kraken.com" and "northflank.com" are the real slugs those companies chose
	// and are legitimate display names. What must not appear is a URL or a path.
	for _, company := range Companies() {
		if strings.HasPrefix(company, "http://") || strings.HasPrefix(company, "https://") {
			t.Errorf("company %q is a URL, want a readable name", company)
		}

		if strings.Contains(company, "/") {
			t.Errorf("company %q contains a path separator, want a readable name", company)
		}

		// A multi-label hostname is the Phenom-style leak this guards against;
		// a single trailing label like "kraken.com" is a real board slug.
		if strings.Count(company, ".") > 1 {
			t.Errorf("company %q looks like a hostname, want a readable name", company)
		}
	}
}

func TestCompaniesAreSortedAndDeduplicated(t *testing.T) {
	t.Parallel()

	companies := Companies()

	if len(companies) < 100 {
		t.Fatalf("got %d companies, want a substantial list", len(companies))
	}

	for i := 1; i < len(companies); i++ {
		previous, current := strings.ToLower(companies[i-1]), strings.ToLower(companies[i])

		if previous > current {
			t.Errorf("companies are not sorted: %q precedes %q", companies[i-1], companies[i])

			break
		}

		if previous == current {
			t.Errorf("duplicate company %q", companies[i])

			break
		}
	}
}

func TestSourcesMatchingAcceptsEitherIdentity(t *testing.T) {
	t.Parallel()

	// A user should be able to name a company either way. Matching only one form
	// meant whichever the user did not happen to pick selected nothing.
	var sample Source

	for _, source := range Builtin {
		if source.Key != source.Company {
			sample = source

			break
		}
	}

	if sample.Key == "" {
		t.Skip("no source with a distinct key is registered")
	}

	byName := SourcesMatching([]string{sample.Company})
	if len(byName) == 0 {
		t.Errorf("SourcesMatching(%q) found nothing by display name", sample.Company)
	}

	byKey := SourcesMatching([]string{sample.Key})
	if len(byKey) == 0 {
		t.Errorf("SourcesMatching(%q) found nothing by ATS key", sample.Key)
	}
}

func TestSourcesMatchingIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	if len(Builtin) == 0 {
		t.Skip("no sources registered")
	}

	company := Builtin[0].Company

	if got := SourcesMatching([]string{strings.ToUpper(company)}); len(got) == 0 {
		t.Errorf("SourcesMatching(%q) found nothing when upper-cased", company)
	}
}

func TestSourcesMatchingEmptyTermsReturnsEverything(t *testing.T) {
	t.Parallel()

	if got := SourcesMatching(nil); len(got) != len(Builtin) {
		t.Errorf("SourcesMatching(nil) returned %d sources, want all %d", len(got), len(Builtin))
	}

	// A list of only blank terms is not a constraint either.
	if got := SourcesMatching([]string{"", "  "}); len(got) != len(Builtin) {
		t.Errorf("SourcesMatching(blanks) returned %d sources, want all %d", len(got), len(Builtin))
	}
}

func TestSourcesMatchingUnknownCompanyReturnsNothing(t *testing.T) {
	t.Parallel()

	if got := SourcesMatching([]string{"definitely-not-a-real-company-xyzzy"}); len(got) != 0 {
		t.Errorf("SourcesMatching(unknown) returned %d sources, want 0", len(got))
	}
}

func TestJobsFuncsMatchesSourceCount(t *testing.T) {
	t.Parallel()

	sources := Builtin[:min(10, len(Builtin))]

	if got := JobsFuncs(sources); len(got) != len(sources) {
		t.Errorf("JobsFuncs returned %d functions, want %d", len(got), len(sources))
	}
}
