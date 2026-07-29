package mcp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The tests here exercise the wiring between this package and the crawler's
// registry. None of them fetch: they cover selection and identity, which are
// what the bound is computed from, and which are exactly the parts that would
// otherwise only be verified by a live crawl.

func TestBuiltinCatalogCoversTheWholeRegistry(t *testing.T) {
	t.Parallel()

	catalog := NewBuiltinCatalog(http.DefaultClient, 1)

	test.Eq(t, len(services.Builtin), len(catalog.Sources()),
		test.Sprint("the catalog must expose every registered source"))
	test.SliceNotEmpty(t, catalog.Sources())
}

func TestBuiltinCatalogPreservesSourceIdentity(t *testing.T) {
	t.Parallel()

	// Platform, key and company are three separate concepts, and the crawler has
	// already had a bug from conflating the last two. Copying them into this
	// package's Source must not lose or reorder them.
	catalog := NewBuiltinCatalog(http.DefaultClient, 1)

	registered := make(map[string]services.Source, len(services.Builtin))
	for _, source := range services.Builtin {
		registered[source.Platform+"\x00"+source.Key] = source
	}

	for _, source := range catalog.Sources() {
		original, ok := registered[source.Platform+"\x00"+source.Key]
		must.True(t, ok, must.Sprintf("catalog invented a source %q/%q", source.Platform, source.Key))
		test.Eq(t, original.Company, source.Company)

		test.NotEq(t, "", source.Platform)
		test.NotEq(t, "", source.Key)
	}
}

func TestBuiltinCatalogSelectNarrows(t *testing.T) {
	t.Parallel()

	// Narrowing is the whole cost model: this is what makes a bounded search
	// possible, and it must select strictly fewer boards than the registry.
	catalog := NewBuiltinCatalog(http.DefaultClient, 1)

	selected := catalog.Select([]string{"anthropic"})

	must.SliceNotEmpty(t, selected, must.Sprint("anthropic must be in the builtin registry"))
	test.Less(t, len(catalog.Sources()), len(selected))

	for _, source := range selected {
		test.True(t,
			containsFold(source.Company, "anthropic") || containsFold(source.Key, "anthropic"),
			test.Sprintf("source %q/%q does not match the term", source.Platform, source.Key))
	}
}

func TestBuiltinCatalogSelectMatchesTheCLI(t *testing.T) {
	t.Parallel()

	// If this drifted from services.SourcesMatching, the count the bound is
	// checked against would stop describing what a crawl would actually fetch.
	catalog := NewBuiltinCatalog(http.DefaultClient, 1)

	for _, term := range []string{"anthropic", "stripe", "data"} {
		test.Eq(t, len(services.SourcesMatching([]string{term})), len(catalog.Select([]string{term})),
			test.Sprintf("selection for %q differs from the CLI's", term))
	}
}

func TestBuiltinCatalogSelectUnknownReturnsNothing(t *testing.T) {
	t.Parallel()

	catalog := NewBuiltinCatalog(http.DefaultClient, 1)

	test.SliceEmpty(t, catalog.Select([]string{"no-such-company-exists-anywhere"}))
}

func TestBuiltinCatalogClampsConcurrency(t *testing.T) {
	t.Parallel()

	// A concurrency of zero would make the crawl never start rather than run
	// sequentially.
	test.Eq(t, 1, NewBuiltinCatalog(http.DefaultClient, 0).concurrency)
	test.Eq(t, 1, NewBuiltinCatalog(http.DefaultClient, -5).concurrency)
	test.Eq(t, 8, NewBuiltinCatalog(http.DefaultClient, 8).concurrency)
}

func TestBuiltinCatalogRefusesUnboundedSearchAtRealScale(t *testing.T) {
	t.Parallel()

	// The bound has to hold against the real registry, not only against a
	// four-source fixture. With the default limit, a term matching the whole
	// registry must be refused rather than crawled.
	catalog := NewBuiltinCatalog(http.DefaultClient, 1)

	server := &Server{Catalog: catalog, Version: "test"}

	result := callSearch(t, server, map[string]any{})
	test.True(t, result.IsError, test.Sprint("an unbounded search must be refused at real scale"))

	// And a term broad enough to select more than the budget must be refused
	// too. Every source matches the empty-ish term "a" somewhere in its name or
	// key far more often than DefaultMaxSources allows.
	broad := callSearch(t, server, map[string]any{"companies": []string{"a"}})
	test.True(t, broad.IsError, test.Sprint("an over-broad search must be refused at real scale"))
	test.StrContains(t, resultText(broad), "more than the")
}

// containsFold is a case-insensitive substring test.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
