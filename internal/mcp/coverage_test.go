package mcp

import (
	"encoding/json"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// companyPayload decodes the structured content of list_companies.
func companyPayload(t *testing.T, result toolResult) companyListResult {
	t.Helper()

	test.False(t, result.IsError, test.Sprintf("unexpected tool error: %s", resultText(result)))

	payload, ok := result.StructuredContent.(companyListResult)
	must.True(t, ok, must.Sprintf("expected a companyListResult, got %T", result.StructuredContent))

	return payload
}

func TestListCompaniesGroupsSourcesByCompany(t *testing.T) {
	t.Parallel()

	payload := companyPayload(t, callTool(t, testServer(testCatalog()), "list_companies", map[string]any{}))

	// Four sources across four distinct companies, sorted case-insensitively.
	must.Len(t, 4, payload.Companies)

	names := make([]string, 0, len(payload.Companies))
	for _, entry := range payload.Companies {
		names = append(names, entry.Company)
	}

	test.Eq(t, []string{"acme", "Acme Labs", "globex", "initech"}, names)
	test.Eq(t, 4, payload.TotalCompanies)
	test.Eq(t, 4, payload.TotalSources)
}

func TestListCompaniesCollapsesOneCompanyOnSeveralPlatforms(t *testing.T) {
	t.Parallel()

	// A company registered on two ATS platforms is one entry with two platforms,
	// not two entries. A migration from Greenhouse to Ashby leaves both
	// registered until someone removes the old one.
	catalog := testCatalog()
	catalog.sources = append(catalog.sources, Source{
		Platform: "ashby", Key: "globex", Company: "Globex",
	})

	payload := companyPayload(t, callTool(t, testServer(catalog), "list_companies", map[string]any{
		"contains": []string{"globex"},
	}))

	must.Len(t, 1, payload.Companies)
	test.Eq(t, []string{"ashby", "greenhouse"}, payload.Companies[0].Platforms)
	test.Eq(t, []string{"globex"}, payload.Companies[0].Keys)
}

func TestListCompaniesMatchesTheSameWayAsSearch(t *testing.T) {
	t.Parallel()

	// If the two tools spelled matching differently, list_companies would be
	// useless for finding a search term, which is its whole purpose.
	payload := companyPayload(t, callTool(t, testServer(testCatalog()), "list_companies", map[string]any{
		"contains": []string{"acme"},
	}))

	must.Len(t, 2, payload.Companies)
	test.Eq(t, "acme", payload.Companies[0].Company)
	test.Eq(t, "Acme Labs", payload.Companies[1].Company)
}

func TestListCompaniesFiltersByPlatform(t *testing.T) {
	t.Parallel()

	payload := companyPayload(t, callTool(t, testServer(testCatalog()), "list_companies", map[string]any{
		"platforms": []string{"lever"},
	}))

	must.Len(t, 2, payload.Companies)

	for _, entry := range payload.Companies {
		test.Eq(t, []string{"lever"}, entry.Platforms)
	}
}

func TestListCompaniesPages(t *testing.T) {
	t.Parallel()

	server := testServer(testCatalog())

	first := companyPayload(t, callTool(t, server, "list_companies", map[string]any{"limit": 2}))
	must.Len(t, 2, first.Companies)
	test.Eq(t, 4, first.Matched, test.Sprint("matched describes the whole result, not the page"))
	test.Eq(t, 2, first.Returned)

	second := companyPayload(t, callTool(t, server, "list_companies", map[string]any{"limit": 2, "offset": 2}))
	must.Len(t, 2, second.Companies)
	test.Eq(t, "globex", second.Companies[0].Company)

	// An offset past the end is an empty page, not an error and not a wrapped
	// one.
	past := companyPayload(t, callTool(t, server, "list_companies", map[string]any{"offset": 99}))
	test.SliceEmpty(t, past.Companies)
	test.Eq(t, 4, past.Matched)
}

func TestListCompaniesNeverReturnsNull(t *testing.T) {
	t.Parallel()

	payload := companyPayload(t, callTool(t, testServer(testCatalog()), "list_companies", map[string]any{
		"contains": []string{"nosuchcompany"},
	}))

	must.NotNil(t, payload.Companies)

	encoded, err := json.Marshal(payload)
	must.NoError(t, err)
	test.StrContains(t, string(encoded), `"companies":[]`)
}

func TestListPlatformsCountsSourcesAndCompanies(t *testing.T) {
	t.Parallel()

	result := callTool(t, testServer(testCatalog()), "list_platforms", map[string]any{})

	payload, ok := result.StructuredContent.(platformListResult)
	must.True(t, ok, must.Sprintf("expected a platformListResult, got %T", result.StructuredContent))

	must.Len(t, 2, payload.Platforms)

	// Sorted by platform name, so the answer does not depend on map order.
	test.Eq(t, "greenhouse", payload.Platforms[0].Platform)
	test.Eq(t, 2, payload.Platforms[0].Sources)
	test.Eq(t, 2, payload.Platforms[0].Companies)

	test.Eq(t, "lever", payload.Platforms[1].Platform)
	test.Eq(t, 2, payload.Platforms[1].Sources)

	test.Eq(t, 4, payload.TotalSources)
	test.Eq(t, 4, payload.TotalCompanies)
}

func TestListPlatformsSeparatesSourcesFromCompanies(t *testing.T) {
	t.Parallel()

	// One employer with two tenants on one ATS is two sources and one company.
	// Reporting either number as the other would misstate coverage.
	catalog := testCatalog()
	catalog.sources = append(catalog.sources, Source{
		Platform: "greenhouse", Key: "globex-eu", Company: "globex",
	})

	result := callTool(t, testServer(catalog), "list_platforms", map[string]any{})

	payload, ok := result.StructuredContent.(platformListResult)
	must.True(t, ok)

	test.Eq(t, 3, payload.Platforms[0].Sources)
	test.Eq(t, 2, payload.Platforms[0].Companies)
}

func TestListPlatformsRejectsStrayArguments(t *testing.T) {
	t.Parallel()

	rpcErr := decodeArgs(json.RawMessage(`{"platform":"lever"}`), &struct{}{})

	must.NotNil(t, rpcErr)
	test.Eq(t, codeInvalidParams, rpcErr.Code)
}

func TestRegistryToolsMakeNoRequests(t *testing.T) {
	t.Parallel()

	// The two registry tools are documented as answering immediately from data
	// compiled into the binary. If either ever reached the catalog's Crawl, that
	// promise would be silently false.
	catalog := testCatalog()
	server := testServer(catalog)

	callTool(t, server, "list_companies", map[string]any{})
	callTool(t, server, "list_platforms", map[string]any{})
	callTool(t, server, "lookup_employer", map[string]any{"companies": []string{"acme"}})

	test.SliceEmpty(t, catalog.crawled, test.Sprint("registry tools must not crawl"))
}
