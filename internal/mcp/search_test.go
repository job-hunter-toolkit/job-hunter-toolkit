package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// callSearch invokes search_jobs with the given arguments and returns the raw
// tool result.
func callSearch(t *testing.T, s *Server, args map[string]any) toolResult {
	t.Helper()

	return callTool(t, s, "search_jobs", args)
}

// callTool invokes a tool through the same tools/call path a client uses, so
// the tests exercise dispatch and argument decoding rather than the handlers
// alone.
func callTool(t *testing.T, s *Server, name string, args map[string]any) toolResult {
	t.Helper()

	encodedArgs, err := json.Marshal(args)
	must.NoError(t, err)

	params, err := json.Marshal(callToolParams{Name: name, Arguments: encodedArgs})
	must.NoError(t, err)

	result, rpcErr := s.handle(context.Background(), request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/call",
		Params:  params,
	})
	must.Nil(t, rpcErr, must.Sprintf("unexpected protocol error: %v", rpcErr))

	tool, ok := result.(toolResult)
	must.True(t, ok, must.Sprintf("expected a toolResult, got %T", result))

	return tool
}

// searchPayload decodes the structured content of a successful search.
func searchPayload(t *testing.T, result toolResult) searchResult {
	t.Helper()

	test.False(t, result.IsError, test.Sprintf("unexpected tool error: %s", resultText(result)))

	payload, ok := result.StructuredContent.(searchResult)
	must.True(t, ok, must.Sprintf("expected a searchResult, got %T", result.StructuredContent))

	return payload
}

// resultText joins the text blocks of a tool result.
func resultText(result toolResult) string {
	var b strings.Builder

	for _, content := range result.Content {
		b.WriteString(content.Text)
	}

	return b.String()
}

func TestSearchRefusesWithoutCompanies(t *testing.T) {
	t.Parallel()

	// The central promise of this server: an unbounded search does not run. A
	// crawl of every board takes about fifteen minutes, so answering this by
	// starting one would hang the agent rather than help it.
	catalog := testCatalog()
	result := callSearch(t, testServer(catalog), map[string]any{})

	test.True(t, result.IsError, test.Sprint("an unbounded search must be refused"))
	test.StrContains(t, resultText(result), "requires a non-empty \"companies\"")

	// Refusing is only honest if nothing was fetched.
	test.SliceEmpty(t, catalog.crawled, test.Sprint("a refused search must not crawl"))
}

func TestSearchRefusesBlankCompanies(t *testing.T) {
	t.Parallel()

	// A list of blanks is a mistake, not a request to search everything.
	catalog := testCatalog()
	result := callSearch(t, testServer(catalog), map[string]any{
		"companies": []string{"", "   "},
	})

	test.True(t, result.IsError)
	test.StrContains(t, resultText(result), "requires a non-empty \"companies\"")
	test.SliceEmpty(t, catalog.crawled)
}

func TestSearchRefusesTooManySources(t *testing.T) {
	t.Parallel()

	// testServer caps at 3 sources; the fixture has 4, and an empty-ish term
	// like "e" matches all of them.
	catalog := testCatalog()
	result := callSearch(t, testServer(catalog), map[string]any{
		"companies": []string{"e"},
	})

	test.True(t, result.IsError, test.Sprint("an over-broad search must be refused"))

	text := resultText(result)
	test.StrContains(t, text, "matches 4 job boards")
	test.StrContains(t, text, "more than the 3")

	// The refusal must precede the fetch, or the bound is decorative.
	test.SliceEmpty(t, catalog.crawled, test.Sprint("an over-broad search must not crawl"))
}

func TestSearchRefusesUnknownCompany(t *testing.T) {
	t.Parallel()

	catalog := testCatalog()
	result := callSearch(t, testServer(catalog), map[string]any{
		"companies": []string{"nosuchcompany"},
	})

	test.True(t, result.IsError)
	test.StrContains(t, resultText(result), "No job board matches")
	test.StrContains(t, resultText(result), "list_companies")
	test.SliceEmpty(t, catalog.crawled)
}

func TestSearchRefusalsAreToolErrorsNotProtocolErrors(t *testing.T) {
	t.Parallel()

	// A refusal must reach the model as readable text it can act on. Returned as
	// a JSON-RPC error, the sentence explaining which argument to narrow would be
	// hidden behind a transport failure in most clients.
	params, err := json.Marshal(callToolParams{Name: "search_jobs", Arguments: json.RawMessage(`{}`)})
	must.NoError(t, err)

	result, rpcErr := testServer(testCatalog()).handle(context.Background(), request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/call",
		Params:  params,
	})

	must.Nil(t, rpcErr, must.Sprint("a refusal must not be a protocol error"))

	tool, ok := result.(toolResult)
	must.True(t, ok)
	test.True(t, tool.IsError)
}

func TestSearchNarrowsSourcesBeforeCrawling(t *testing.T) {
	t.Parallel()

	// The property the whole design rests on: naming a company selects boards,
	// it does not filter a crawl of everything.
	catalog := testCatalog()

	callSearch(t, testServer(catalog), map[string]any{"companies": []string{"globex"}})

	must.Len(t, 1, catalog.crawled)
	test.Eq(t, "globex", catalog.crawled[0].Key)
}

func TestSearchReturnsSortedPostings(t *testing.T) {
	t.Parallel()

	result := callSearch(t, testServer(testCatalog()), map[string]any{
		"companies": []string{"acme"},
	})

	payload := searchPayload(t, result)

	// Two sources, four postings, sorted by company then title regardless of the
	// order the crawl produced them in.
	titles := make([]string, 0, len(payload.Postings))
	for _, p := range payload.Postings {
		titles = append(titles, p.Company+"/"+p.Title)
	}

	test.Eq(t, []string{
		"acme/Accountant",
		"acme/Backend Engineer",
		"acme/Staff Security Engineer",
		"Acme Labs/Research Engineer",
	}, titles)
}

func TestSearchIsDeterministic(t *testing.T) {
	t.Parallel()

	// Same input, byte-identical output. The crawler yields postings in fetch
	// order, so without the sort this fails as soon as two boards race.
	//
	// One catalog across all three calls, deliberately: the fixture rotates its
	// yield order on every crawl, so a fresh catalog per iteration would reset
	// the rotation and let a missing sort pass.
	var (
		encodings []string
		catalog   = testCatalog()
		server    = testServer(catalog)
	)

	for range 3 {
		result := callSearch(t, server, map[string]any{
			"companies": []string{"acme", "globex"},
		})

		encoded, err := json.Marshal(searchPayload(t, result))
		must.NoError(t, err)

		encodings = append(encodings, string(encoded))
	}

	test.Eq(t, encodings[0], encodings[1])
	test.Eq(t, encodings[0], encodings[2])
}

func TestSearchReportsCompleteWhenEveryBoardAnswered(t *testing.T) {
	t.Parallel()

	payload := searchPayload(t, callSearch(t, testServer(testCatalog()), map[string]any{
		"companies": []string{"globex"},
	}))

	test.True(t, payload.Summary.Complete)
	test.Eq(t, "", payload.Summary.IncompleteReason)
	test.Eq(t, 1, payload.Summary.SourcesSelected)
	test.Eq(t, 0, payload.Summary.SourcesFailed)
	test.Eq(t, 1, payload.Summary.Matched)
	test.Eq(t, 1, payload.Summary.Returned)
	test.False(t, payload.Summary.Truncated)
}

func TestSearchMarksATimedOutCrawlIncomplete(t *testing.T) {
	t.Parallel()

	// A partial crawl is never reported as complete. This is the same invariant
	// the manifests enforce, and it matters more here: an agent that reads an
	// empty result as "nothing is hiring" will say so out loud.
	catalog := testCatalog()
	catalog.block = true

	server := testServer(catalog)
	server.Limits.Timeout = 50 * time.Millisecond

	payload := searchPayload(t, callSearch(t, server, map[string]any{
		"companies": []string{"globex"},
	}))

	test.False(t, payload.Summary.Complete, test.Sprint("a crawl that hit its budget is not complete"))
	test.StrContains(t, payload.Summary.IncompleteReason, "partial")

	// The postings collected before the deadline are still returned: a partial
	// answer is worth more than none, as long as it says it is partial.
	test.Len(t, 1, payload.Postings)
}

func TestSearchSurvivesAFailingBoard(t *testing.T) {
	t.Parallel()

	// One dead board cannot end a search. At this scale some fraction of any
	// selection has been retired since the registry was written.
	catalog := testCatalog()
	catalog.failing = map[string]error{"greenhouse/acme": errBoardRetired}

	payload := searchPayload(t, callSearch(t, testServer(catalog), map[string]any{
		"companies": []string{"acme"},
	}))

	test.Eq(t, 1, payload.Summary.SourcesFailed)
	test.True(t, payload.Summary.Complete)
	must.Len(t, 1, payload.Summary.Errors)
	test.StrContains(t, payload.Summary.Errors[0], "404")

	// The surviving board's postings still came back.
	must.Len(t, 1, payload.Postings)
	test.Eq(t, "Research Engineer", payload.Postings[0].Title)
}

func TestSearchTruncatesAndSaysSo(t *testing.T) {
	t.Parallel()

	server := testServer(testCatalog())

	payload := searchPayload(t, callSearch(t, server, map[string]any{
		"companies": []string{"acme"},
		"limit":     2,
	}))

	test.True(t, payload.Summary.Truncated)
	test.Eq(t, 4, payload.Summary.Matched, test.Sprint("matched counts everything that passed the filter"))
	test.Eq(t, 2, payload.Summary.Returned)
	test.Len(t, 2, payload.Postings)
}

func TestSearchLimitIsCappedAtMaxLimit(t *testing.T) {
	t.Parallel()

	// A client asking for more than the server allows gets the cap, not an error:
	// the request is reasonable, the number is not.
	server := testServer(testCatalog())
	server.Limits.MaxLimit = 2
	server.Limits.DefaultLimit = 1

	payload := searchPayload(t, callSearch(t, server, map[string]any{
		"companies": []string{"acme"},
		"limit":     1000,
	}))

	test.Eq(t, 2, payload.Summary.Returned)
}

func TestSearchAppliesPostingFilters(t *testing.T) {
	t.Parallel()

	payload := searchPayload(t, callSearch(t, testServer(testCatalog()), map[string]any{
		"companies": []string{"acme", "initech"},
		"titles":    []string{"engineer"},
	}))

	for _, p := range payload.Postings {
		test.StrContains(t, strings.ToLower(p.Title), "engineer")
	}

	test.Eq(t, 4, payload.Summary.Matched)
}

func TestSearchAppliesExcludeTitles(t *testing.T) {
	t.Parallel()

	payload := searchPayload(t, callSearch(t, testServer(testCatalog()), map[string]any{
		"companies":      []string{"acme"},
		"titles":         []string{"engineer"},
		"exclude_titles": []string{"security"},
	}))

	titles := make([]string, 0, len(payload.Postings))
	for _, p := range payload.Postings {
		titles = append(titles, p.Title)
	}

	test.Eq(t, []string{"Backend Engineer", "Research Engineer"}, titles)
}

func TestSearchAppliesRemoteFilter(t *testing.T) {
	t.Parallel()

	payload := searchPayload(t, callSearch(t, testServer(testCatalog()), map[string]any{
		"companies": []string{"acme"},
		"remote":    true,
	}))

	must.Len(t, 1, payload.Postings)
	test.Eq(t, "Staff Security Engineer", payload.Postings[0].Title)
}

func TestSearchDoesNotReapplyCompaniesAsAPostingFilter(t *testing.T) {
	t.Parallel()

	// "acme-labs" is the ATS slug; the company display name is "Acme Labs".
	// Re-applying the term as a posting filter would drop every posting it
	// selected, which is the bug the CLI documents.
	payload := searchPayload(t, callSearch(t, testServer(testCatalog()), map[string]any{
		"companies": []string{"acme-labs"},
	}))

	must.Len(t, 1, payload.Postings)
	test.Eq(t, "Acme Labs", payload.Postings[0].Company)
}

func TestSearchReportsWhichCompaniesItSearched(t *testing.T) {
	t.Parallel()

	// A term can select more than the caller expected. Naming what was searched
	// is what lets an agent notice that "acme" also matched "Acme Labs".
	payload := searchPayload(t, callSearch(t, testServer(testCatalog()), map[string]any{
		"companies": []string{"acme"},
	}))

	test.Eq(t, []string{"acme", "Acme Labs"}, payload.Summary.Companies)
}

func TestSearchPostingsAreNeverNull(t *testing.T) {
	t.Parallel()

	payload := searchPayload(t, callSearch(t, testServer(testCatalog()), map[string]any{
		"companies": []string{"globex"},
		"titles":    []string{"nothing matches this"},
	}))

	must.NotNil(t, payload.Postings)
	test.SliceEmpty(t, payload.Postings)

	// And it must serialize as [] rather than null.
	encoded, err := json.Marshal(payload)
	must.NoError(t, err)
	test.StrContains(t, string(encoded), `"postings":[]`)
}

func TestSearchRejectsUnknownArguments(t *testing.T) {
	t.Parallel()

	// An agent writing "company" instead of "companies" must be told, not
	// silently given an unbounded search that is then refused for the wrong
	// reason.
	params, err := json.Marshal(callToolParams{
		Name:      "search_jobs",
		Arguments: json.RawMessage(`{"company":"acme"}`),
	})
	must.NoError(t, err)

	_, rpcErr := testServer(testCatalog()).handle(context.Background(), request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/call",
		Params:  params,
	})

	must.NotNil(t, rpcErr)
	test.Eq(t, codeInvalidParams, rpcErr.Code)
	test.StrContains(t, rpcErr.Message, "company")
}

func TestSearchRejectsUnknownEnumValues(t *testing.T) {
	t.Parallel()

	// The schema publishes a closed enum, so accepting "Full-Time" here would
	// teach an agent a spelling the next client validates and rejects.
	params, err := json.Marshal(callToolParams{
		Name:      "search_jobs",
		Arguments: json.RawMessage(`{"companies":["acme"],"employment_types":["Full-Time"]}`),
	})
	must.NoError(t, err)

	_, rpcErr := testServer(testCatalog()).handle(context.Background(), request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/call",
		Params:  params,
	})

	must.NotNil(t, rpcErr)
	test.StrContains(t, rpcErr.Message, "unknown employment_type")
	test.StrContains(t, rpcErr.Message, "full_time")
}

func TestSearchRejectsBothRecencyArguments(t *testing.T) {
	t.Parallel()

	params, err := json.Marshal(callToolParams{
		Name:      "search_jobs",
		Arguments: json.RawMessage(`{"companies":["acme"],"posted_since":"2026-01-01","posted_within_days":30}`),
	})
	must.NoError(t, err)

	_, rpcErr := testServer(testCatalog()).handle(context.Background(), request{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/call",
		Params:  params,
	})

	must.NotNil(t, rpcErr)
	test.StrContains(t, rpcErr.Message, "not both")
}

func TestPostedSinceAcceptsBothFormats(t *testing.T) {
	t.Parallel()

	dateOnly, rpcErr := searchArgs{PostedSince: "2026-01-31"}.postedSince()
	must.Nil(t, rpcErr)
	test.Eq(t, 2026, dateOnly.Year())
	test.Eq(t, time.January, dateOnly.Month())
	test.Eq(t, 31, dateOnly.Day())

	timestamp, rpcErr := searchArgs{PostedSince: "2026-01-31T12:00:00Z"}.postedSince()
	must.Nil(t, rpcErr)
	test.Eq(t, 12, timestamp.Hour())

	_, rpcErr = searchArgs{PostedSince: "last tuesday"}.postedSince()
	must.NotNil(t, rpcErr)
	test.StrContains(t, rpcErr.Message, "2026-01-31")
}

func TestPostedWithinDaysResolvesToAnInstant(t *testing.T) {
	t.Parallel()

	since, rpcErr := searchArgs{PostedWithinDays: 30}.postedSince()
	must.Nil(t, rpcErr)

	elapsed := time.Since(since)
	test.Greater(t, 29*24*time.Hour, elapsed)
	test.Less(t, 31*24*time.Hour, elapsed)
}
