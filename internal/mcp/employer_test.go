package mcp

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// employerPayload decodes the structured content of lookup_employer.
func employerPayload(t *testing.T, result toolResult) employerLookupResult {
	t.Helper()

	test.False(t, result.IsError, test.Sprintf("unexpected tool error: %s", resultText(result)))

	payload, ok := result.StructuredContent.(employerLookupResult)
	must.True(t, ok, must.Sprintf("expected an employerLookupResult, got %T", result.StructuredContent))

	return payload
}

func TestLookupEmployerReturnsAReviewedRow(t *testing.T) {
	t.Parallel()

	payload := employerPayload(t, callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{
		"companies": []string{"acme"},
	}))

	// The slug "acme" on greenhouse is the one source in the fixture with a
	// reviewed row, and every published fact travels with it. It sorts first.
	must.SliceNotEmpty(t, payload.Employers)

	entry := payload.Employers[0]
	must.Eq(t, "greenhouse", entry.Source.Platform)
	must.Eq(t, "acme", entry.Source.Key)

	test.True(t, entry.Known)
	must.NotNil(t, entry.Employer)
	test.Eq(t, "Acme Corporation", entry.Employer.LegalName)
	test.Eq(t, "0000000001", entry.Employer.CIK)
	test.Eq(t, "NYSE", entry.Employer.Exchange)
	test.Eq(t, "7372 Prepackaged Software", entry.Employer.Industry)
	test.Eq(t, 1234, entry.Employer.Employees)
	test.Eq(t, "Springfield, USA", entry.Employer.Headquarters)

	// Public is a pointer because "privately held" and "nobody checked" are
	// different answers.
	must.NotNil(t, entry.Employer.Public)
	test.True(t, *entry.Employer.Public)
}

func TestLookupEmployerJoinsOnPlatformAndKey(t *testing.T) {
	t.Parallel()

	// The join key is the integration ID, never the display name. "acme" and
	// "Acme Labs" are different employers on different platforms, and only the
	// first has a reviewed row.
	payload := employerPayload(t, callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{
		"companies": []string{"acme"},
	}))

	must.Len(t, 2, payload.Employers)

	test.Eq(t, "acme", payload.Employers[0].Source.Company)
	test.True(t, payload.Employers[0].Known)
	must.NotNil(t, payload.Employers[0].Employer)
	test.Eq(t, "Acme Corporation", payload.Employers[0].Employer.LegalName)
	test.Eq(t, "ACME", payload.Employers[0].Employer.Ticker)

	test.Eq(t, "Acme Labs", payload.Employers[1].Source.Company)
	test.False(t, payload.Employers[1].Known)

	test.Eq(t, 2, payload.Matched)
	test.Eq(t, 1, payload.Known)
}

func TestLookupEmployerReportsUnresolvedAsAFactNotAGap(t *testing.T) {
	t.Parallel()

	// A company with no row must still appear, marked unknown. Omitting it would
	// make "nobody has resolved this" indistinguishable from "this board is not
	// covered", and the difference is exactly what a caller needs.
	payload := employerPayload(t, callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{
		"companies": []string{"initech"},
	}))

	must.Len(t, 1, payload.Employers)
	test.False(t, payload.Employers[0].Known)
	test.Nil(t, payload.Employers[0].Employer)
	test.Eq(t, "initech", payload.Employers[0].Source.Company)
}

func TestLookupEmployerCarriesMatchProvenance(t *testing.T) {
	t.Parallel()

	// A row joined by SEC identifier and one joined because two names happened
	// to be equal are not equally trustworthy, so the method, confidence and
	// retrieval date travel with the facts.
	payload := employerPayload(t, callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{
		"companies": []string{"acme"},
	}))

	var found bool

	for _, entry := range payload.Employers {
		if entry.Employer == nil {
			continue
		}

		found = true

		test.Eq(t, "manual", string(entry.Employer.Match.Method))
		test.Eq(t, "high", string(entry.Employer.Match.Confidence))
		test.Eq(t, "2026-07-01", entry.Employer.Match.RetrievedAt)
		test.Eq(t, []string{"sec-edgar"}, entry.Employer.Match.DataSources)
	}

	test.True(t, found, test.Sprint("expected at least one reviewed row"))
}

func TestLookupEmployerReportsTableSize(t *testing.T) {
	t.Parallel()

	// "known: 0" is ambiguous without this: it could mean unresolved companies
	// or a table that failed to load.
	payload := employerPayload(t, callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{
		"companies": []string{"initech"},
	}))

	test.Eq(t, 1, payload.TableRows)
}

func TestLookupEmployerRefusesWithoutCompanies(t *testing.T) {
	t.Parallel()

	result := callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{})

	test.True(t, result.IsError)
	test.StrContains(t, resultText(result), "requires a non-empty \"companies\"")
}

func TestLookupEmployerRefusesUnknownCompany(t *testing.T) {
	t.Parallel()

	result := callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{
		"companies": []string{"nosuchcompany"},
	})

	test.True(t, result.IsError)
	test.StrContains(t, resultText(result), "No job board matches")
}

func TestLookupEmployerSaysWhenNoTableIsLoaded(t *testing.T) {
	t.Parallel()

	// A missing table is a server configuration problem. Saying so is important:
	// reported as "nothing known about acme", it would read as a fact about the
	// company.
	server := testServer(testCatalog())
	server.Employers = nil

	result := callTool(t, server, "lookup_employer", map[string]any{
		"companies": []string{"acme"},
	})

	test.True(t, result.IsError)
	test.StrContains(t, resultText(result), "No employer table is loaded")
	test.StrContains(t, resultText(result), "not a fact about the companies")
}

func TestLookupEmployerIsDeterministic(t *testing.T) {
	t.Parallel()

	// Sorted before truncation, so a limited reply is the same first page every
	// time rather than whichever boards the registry happened to list first.
	server := testServer(testCatalog())

	first := employerPayload(t, callTool(t, server, "lookup_employer", map[string]any{
		"companies": []string{"acme"}, "limit": 1,
	}))
	second := employerPayload(t, callTool(t, server, "lookup_employer", map[string]any{
		"companies": []string{"acme"}, "limit": 1,
	}))

	must.Len(t, 1, first.Employers)
	test.Eq(t, first.Employers[0].Source.Key, second.Employers[0].Source.Key)

	// Matched still counts everything the terms selected, so a truncated reply
	// does not understate coverage.
	test.Eq(t, 2, first.Matched)
	test.Eq(t, 1, first.Returned)
}

func TestLookupEmployerCountsKnownBeyondTheReturnedPage(t *testing.T) {
	t.Parallel()

	// Known must describe the whole match, not the page, or a limit of 1 would
	// make a well-covered selection look unresolved.
	payload := employerPayload(t, callTool(t, testServer(testCatalog()), "lookup_employer", map[string]any{
		"companies": []string{"acme"}, "limit": 1,
	}))

	test.Eq(t, 2, payload.Matched)
	test.Eq(t, 1, payload.Known)
	test.Eq(t, 1, payload.Returned)
}
