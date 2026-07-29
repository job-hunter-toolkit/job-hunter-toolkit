package mcp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestToolsListReportsEveryTool(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	must.Len(t, 1, responses)
	must.Nil(t, responses[0].Error)

	encoded, err := json.Marshal(responses[0].Result)
	must.NoError(t, err)

	var result toolListResult

	must.NoError(t, json.Unmarshal(encoded, &result))

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}

	test.Eq(t, []string{"search_jobs", "list_companies", "list_platforms", "lookup_employer"}, names)
}

func TestEveryAdvertisedToolIsCallable(t *testing.T) {
	t.Parallel()

	// A tool listed but not routed would be discoverable and permanently broken.
	server := testServer(testCatalog())

	for _, name := range server.toolNames() {
		params, err := json.Marshal(callToolParams{Name: name, Arguments: json.RawMessage(`{}`)})
		must.NoError(t, err)

		_, rpcErr := server.callTool(t.Context(), params)

		// Refusals are results, not protocol errors, so any rpcErr here means
		// the tool was not routed at all.
		test.Nil(t, rpcErr, test.Sprintf("tool %q is advertised but not routed", name))
	}
}

// findTool returns the named tool from the server's surface.
func findTool(t *testing.T, s *Server, name string) tool {
	t.Helper()

	for _, candidate := range s.tools() {
		if candidate.Name == name {
			return candidate
		}
	}

	t.Fatalf("tool %q is not advertised", name)

	return tool{}
}

func TestEveryToolIsDescribed(t *testing.T) {
	t.Parallel()

	// The descriptions are the entire interface: an agent picks and fills a tool
	// from this text and nothing else. An undescribed argument gets guessed at.
	for _, tool := range testServer(testCatalog()).tools() {
		test.NotEq(t, "", tool.Name)
		test.NotEq(t, "", tool.Title, test.Sprintf("tool %q has no title", tool.Name))
		test.Greater(t, 100, len(tool.Description),
			test.Sprintf("tool %q has a description too short to be useful", tool.Name))

		must.NotNil(t, tool.InputSchema, must.Sprintf("tool %q has no input schema", tool.Name))
		test.Eq(t, "object", tool.InputSchema.Type)

		for name, property := range tool.InputSchema.Properties {
			test.NotEq(t, "", property.Description,
				test.Sprintf("tool %q argument %q has no description", tool.Name, name))
			test.NotEq(t, "", property.Type,
				test.Sprintf("tool %q argument %q has no type", tool.Name, name))
		}
	}
}

func TestEveryRequiredArgumentExists(t *testing.T) {
	t.Parallel()

	// A required name with no matching property is a schema that no client can
	// satisfy.
	for _, tool := range testServer(testCatalog()).tools() {
		for _, required := range tool.InputSchema.Required {
			_, ok := tool.InputSchema.Properties[required]
			test.True(t, ok, test.Sprintf("tool %q requires %q, which it does not define", tool.Name, required))
		}
	}
}

func TestSearchToolDeclaresCompaniesRequired(t *testing.T) {
	t.Parallel()

	// The bound has to be discoverable before the call, not only enforced after
	// it. A client that validates arguments should reject an unbounded search
	// without a round trip.
	search := findTool(t, testServer(testCatalog()), "search_jobs")

	test.Eq(t, []string{"companies"}, search.InputSchema.Required)

	// And the description must say why, in words, because a required field alone
	// does not explain that naming a company is what makes the call affordable.
	test.StrContains(t, search.Description, "REQUIRES")
	test.StrContains(t, search.Description, "WILL NOT")
	test.StrContains(t, search.Description, "list_companies")
}

func TestToolSchemasAreClosed(t *testing.T) {
	t.Parallel()

	// additionalProperties:false is what turns a misspelled argument into a
	// client-side error instead of a silently ignored one, matching the strict
	// decoding on the server side.
	for _, tool := range testServer(testCatalog()).tools() {
		must.NotNil(t, tool.InputSchema.AdditionalProperties,
			must.Sprintf("tool %q does not declare additionalProperties", tool.Name))
		test.False(t, *tool.InputSchema.AdditionalProperties,
			test.Sprintf("tool %q accepts unknown arguments", tool.Name))
	}
}

func TestSchemaEnumsMatchTheFilterVocabulary(t *testing.T) {
	t.Parallel()

	// The list an agent is shown must not drift from the list the filter
	// accepts, or a schema-valid call gets rejected by the server.
	search := findTool(t, testServer(testCatalog()), "search_jobs")

	employment := search.InputSchema.Properties["employment_types"]
	must.NotNil(t, employment)
	must.NotNil(t, employment.Items)

	for _, value := range employment.Items.Enum {
		_, rpcErr := parseEmploymentTypes([]string{value})
		test.Nil(t, rpcErr, test.Sprintf("schema offers employment_type %q that the filter rejects", value))
	}

	test.Eq(t, len(jobposting.EmploymentTypeValues()), len(employment.Items.Enum))

	workplace := search.InputSchema.Properties["workplace_types"]
	must.NotNil(t, workplace)
	must.NotNil(t, workplace.Items)

	for _, value := range workplace.Items.Enum {
		_, rpcErr := parseWorkplaceTypes([]string{value})
		test.Nil(t, rpcErr, test.Sprintf("schema offers workplace_type %q that the filter rejects", value))
	}

	test.Eq(t, len(jobposting.WorkplaceTypeValues()), len(workplace.Items.Enum))
}

func TestSearchArgumentsCoverTheFilterVocabulary(t *testing.T) {
	t.Parallel()

	// query.Query is the shared filter vocabulary. A field it grows that this
	// tool never exposes is a capability the CLI has and agents silently do not,
	// which is the drift the package was written to prevent.
	//
	// Companies is the exception: it selects which boards are fetched rather
	// than filtering postings, so it is spelled the same but lives outside the
	// filter this tool builds.
	search := findTool(t, testServer(testCatalog()), "search_jobs")

	for _, field := range []string{
		"titles", "exclude_titles", "locations", "departments",
		"remote", "has_compensation", "min_annual",
		"employment_types", "workplace_types", "posted_since",
	} {
		_, ok := search.InputSchema.Properties[field]
		test.True(t, ok, test.Sprintf("search_jobs does not expose the %q filter", field))
	}
}

func TestToolSchemasSerializeAsValidJSONSchema(t *testing.T) {
	t.Parallel()

	// The schemas travel over the wire; a field that marshals to something a
	// client cannot read is only visible here.
	encoded, err := json.MarshalIndent(toolListResult{Tools: testServer(testCatalog()).tools()}, "", "  ")
	must.NoError(t, err)

	var decoded struct {
		Tools []wireTool `json:"tools"`
	}

	must.NoError(t, json.Unmarshal(encoded, &decoded))
	must.Len(t, 4, decoded.Tools)

	for _, tool := range decoded.Tools {
		test.Eq(t, "object", tool.InputSchema.Type)
		test.NotEq(t, "", tool.Description)
		test.True(t, tool.Annotations.ReadOnlyHint,
			test.Sprintf("tool %q is not marked read-only", tool.Name))
		test.False(t, tool.InputSchema.AdditionalProperties,
			test.Sprintf("tool %q accepts unknown arguments on the wire", tool.Name))
	}

	// The tool that takes no arguments must still publish an object schema
	// rather than a bare type, and must require nothing.
	index := slices.IndexFunc(decoded.Tools, func(tool wireTool) bool {
		return tool.Name == "list_platforms"
	})

	must.GreaterEq(t, 0, index)
	test.SliceEmpty(t, decoded.Tools[index].InputSchema.Required)
}

func TestEveryToolPublishesAPropertiesMap(t *testing.T) {
	t.Parallel()

	// A tool taking no arguments must still publish "properties": {}, not omit
	// the key. Several MCP clients mishandle a bare {"type":"object"}, and
	// list_platforms is exactly that case.
	encoded, err := json.Marshal(toolListResult{Tools: testServer(testCatalog()).tools()})
	must.NoError(t, err)

	// The schema is decoded as a raw map so that an omitted key is
	// distinguishable from an empty one, which is the entire point of this test.
	var decoded struct {
		Tools []struct {
			Name        string                     `json:"name"`
			InputSchema map[string]json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}

	must.NoError(t, json.Unmarshal(encoded, &decoded))
	must.SliceNotEmpty(t, decoded.Tools)

	for _, tool := range decoded.Tools {
		properties, ok := tool.InputSchema["properties"]
		test.True(t, ok, test.Sprintf("tool %q omits the properties key", tool.Name))
		test.NotEq(t, "null", string(properties),
			test.Sprintf("tool %q publishes a null properties map", tool.Name))
	}
}

// wireTool is a tools/list entry decoded the way a client sees it, rather than
// through this package's own types, so a marshalling mistake is visible.
type wireTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	InputSchema struct {
		Type                 string                     `json:"type"`
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	} `json:"inputSchema"`

	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

func TestToolDescriptionsQuoteRealCoverage(t *testing.T) {
	t.Parallel()

	// The counts in the descriptions come from the catalog rather than from a
	// constant someone has to remember to update.
	catalog := testCatalog()
	server := testServer(catalog)

	test.StrContains(t, findTool(t, server, "list_companies").Description, "Coverage is 4 job boards")

	catalog.sources = append(catalog.sources, Source{Platform: "ashby", Key: "new", Company: "new"})

	test.StrContains(t, findTool(t, server, "list_companies").Description, "Coverage is 5 job boards")
}

func TestLimitsFillInDefaults(t *testing.T) {
	t.Parallel()

	limits := Limits{}.withDefaults()

	test.Eq(t, DefaultMaxSources, limits.MaxSources)
	test.Eq(t, DefaultTimeout, limits.Timeout)
	test.Eq(t, DefaultPostingLimit, limits.DefaultLimit)
	test.Eq(t, MaxPostingLimit, limits.MaxLimit)

	// A default above the maximum would hand out more than the cap allows.
	clamped := Limits{DefaultLimit: 900, MaxLimit: 10}.withDefaults()
	test.Eq(t, 10, clamped.DefaultLimit)
}

func TestConcurrencyDefaultIsPolite(t *testing.T) {
	t.Parallel()

	// A crawl driven by a conversation is still a crawl of somebody else's job
	// board. This is well below the background crawler's width on purpose.
	test.LessEq(t, 8, DefaultConcurrency)
}

func TestInstructionsStateTheCostModel(t *testing.T) {
	t.Parallel()

	// The one thing a model cannot work out by reading four tool descriptions is
	// why search_jobs insists on a company, so the session instructions say it
	// once, up front.
	for _, phrase := range []string{"companies", "list_companies", "real time"} {
		test.StrContains(t, strings.ToLower(instructions), strings.ToLower(phrase))
	}
}
