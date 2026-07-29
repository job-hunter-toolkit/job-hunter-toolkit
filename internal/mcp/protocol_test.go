package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// session runs the server over an in-memory stream and returns the decoded
// responses, which is as close to a real client as this package gets without a
// subprocess.
func session(t *testing.T, s *Server, messages ...string) []response {
	t.Helper()

	var out strings.Builder

	err := s.Run(context.Background(), strings.NewReader(strings.Join(messages, "\n")), &out)
	must.NoError(t, err)

	var (
		decoder   = json.NewDecoder(strings.NewReader(out.String()))
		responses []response
	)

	for decoder.More() {
		var res response

		must.NoError(t, decoder.Decode(&res))

		responses = append(responses, res)
	}

	return responses
}

func TestInitializeAnswersTheHandshake(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`)

	must.Len(t, 1, responses)
	must.Nil(t, responses[0].Error)

	var result initializeResult

	encoded, err := json.Marshal(responses[0].Result)
	must.NoError(t, err)
	must.NoError(t, json.Unmarshal(encoded, &result))

	test.Eq(t, "2025-06-18", result.ProtocolVersion)
	test.Eq(t, "job-hunter-toolkit", result.ServerInfo.Name)
	mustNotBeNil(t, result.Capabilities.Tools, "a tools capability")

	// The instructions are where the cost model is stated once, so a model does
	// not have to infer it from four tool descriptions.
	test.StrContains(t, result.Instructions, "list_companies")
	test.StrContains(t, result.Instructions, "companies")
}

func TestInitializeNegotiatesTheProtocolVersion(t *testing.T) {
	t.Parallel()

	// A version we know is echoed; one we do not is answered with ours, so the
	// client can decide whether it can proceed rather than guess.
	for _, tc := range []struct {
		client string
		want   string
	}{
		{client: "2025-06-18", want: "2025-06-18"},
		{client: "2024-11-05", want: "2024-11-05"},
		{client: "2026-07-28", want: "2026-07-28"},
		{client: "1999-01-01", want: defaultProtocolVersion},
		{client: "", want: defaultProtocolVersion},
	} {
		test.Eq(t, tc.want, negotiateVersion(tc.client),
			test.Sprintf("client asked for %q", tc.client))
	}
}

func TestNotificationsAreNotAnswered(t *testing.T) {
	t.Parallel()

	// A client that receives a response to a notification sees a reply to an id
	// it never issued.
	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`)

	test.SliceEmpty(t, responses)
}

func TestUnknownNotificationIsStillNotAnswered(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","method":"notifications/somethingNew"}`)

	test.SliceEmpty(t, responses)
}

func TestPingIsAnswered(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`)

	must.Len(t, 1, responses)
	must.Nil(t, responses[0].Error)
	test.Eq(t, "7", string(responses[0].ID))
}

func TestUnknownMethodIsAProtocolError(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`)

	must.Len(t, 1, responses)
	must.NotNil(t, responses[0].Error)
	test.Eq(t, codeMethodNotFound, responses[0].Error.Code)
}

func TestRequestIDsAreEchoedExactly(t *testing.T) {
	t.Parallel()

	// JSON-RPC permits a string or a number and requires the response to echo it
	// unchanged. Decoding into any would turn a large integer into a float and
	// send back something the client cannot match to its request.
	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":"abc","method":"ping"}`,
		`{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`)

	must.Len(t, 2, responses)
	test.Eq(t, `"abc"`, string(responses[0].ID))
	test.Eq(t, "9007199254740993", string(responses[1].ID))
}

func TestWrongJSONRPCVersionIsRejected(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"1.0","id":3,"method":"ping"}`)

	must.Len(t, 1, responses)
	must.NotNil(t, responses[0].Error)
	test.Eq(t, codeInvalidRequest, responses[0].Error.Code)
}

func TestMalformedJSONIsReportedAsAParseError(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()), `{"jsonrpc":"2.0","method":}`)

	must.Len(t, 1, responses)
	must.NotNil(t, responses[0].Error)
	test.Eq(t, codeParseError, responses[0].Error.Code)

	// JSON-RPC requires a null id when the request's own id could not be read.
	test.Eq(t, "null", string(responses[0].ID))
}

func TestATruncatedMessageEndsTheSessionQuietly(t *testing.T) {
	t.Parallel()

	// A message cut off at end of stream is a client that died mid-write, not a
	// client sending bad JSON. There is nothing to report it to — the pipe is
	// already gone — so the session ends the same way a clean close does.
	responses := session(t, testServer(testCatalog()), `{"jsonrpc":"2.0","id":1,`)

	test.SliceEmpty(t, responses)
}

func TestOneBadMessageDoesNotSpin(t *testing.T) {
	t.Parallel()

	// A malformed message leaves the decoder stopped mid-value with no way to
	// find where the next one begins. Stopping is the only safe move; retrying
	// would loop on the same bytes forever.
	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{oops}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`)

	must.Len(t, 2, responses)
	test.Eq(t, "1", string(responses[0].ID))
	must.NotNil(t, responses[1].Error)
	test.Eq(t, codeParseError, responses[1].Error.Code)
}

func TestSessionEndsWhenStdinCloses(t *testing.T) {
	t.Parallel()

	// An empty stream is a client that connected and closed. That is the normal
	// end of an MCP stdio session, not a failure.
	responses := session(t, testServer(testCatalog()))

	test.SliceEmpty(t, responses)
}

func TestSeveralRequestsAreServedInOrder(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`)

	must.Len(t, 3, responses, must.Sprint("the notification must not produce a response"))
	test.Eq(t, "1", string(responses[0].ID))
	test.Eq(t, "2", string(responses[1].ID))
	test.Eq(t, "3", string(responses[2].ID))
}

func TestUnknownToolIsAProtocolError(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_jobs","arguments":{}}}`)

	must.Len(t, 1, responses)
	must.NotNil(t, responses[0].Error)
	test.Eq(t, codeInvalidParams, responses[0].Error.Code)

	// The message names the real tools, so a mistaken call can be corrected
	// without a second round trip to tools/list.
	test.StrContains(t, responses[0].Error.Message, "search_jobs")
}

func TestEndToEndSearchOverTheWire(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_jobs","arguments":{"companies":["globex"]}}}`)

	must.Len(t, 2, responses)
	must.Nil(t, responses[1].Error)

	// Decode the way a client would, from the JSON on the wire rather than from
	// the Go value the handler happened to return.
	encoded, err := json.Marshal(responses[1].Result)
	must.NoError(t, err)

	var result struct {
		Content []textContent `json:"content"`
		IsError bool          `json:"isError"`

		StructuredContent struct {
			Postings []struct {
				Company string `json:"company"`
				Title   string `json:"title"`
			} `json:"postings"`
			Summary searchSummary `json:"summary"`
		} `json:"structuredContent"`
	}

	must.NoError(t, json.Unmarshal(encoded, &result))

	test.False(t, result.IsError)
	must.Len(t, 1, result.StructuredContent.Postings)
	test.Eq(t, "Sales Director", result.StructuredContent.Postings[0].Title)
	test.True(t, result.StructuredContent.Summary.Complete)

	// The specification asks that a tool returning structuredContent also put the
	// serialized JSON in a text block, so a client that reads only content still
	// receives the answer.
	must.SliceNotEmpty(t, result.Content)
	test.Eq(t, "text", result.Content[0].Type)
	test.StrContains(t, result.Content[0].Text, "Sales Director")
}

func TestRefusalTravelsAsAToolErrorOverTheWire(t *testing.T) {
	t.Parallel()

	responses := session(t, testServer(testCatalog()),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_jobs","arguments":{}}}`)

	must.Len(t, 1, responses)
	must.Nil(t, responses[0].Error, must.Sprint("a refusal is a result, not a protocol error"))

	encoded, err := json.Marshal(responses[0].Result)
	must.NoError(t, err)

	var result toolResult

	must.NoError(t, json.Unmarshal(encoded, &result))

	test.True(t, result.IsError)
	must.SliceNotEmpty(t, result.Content)
	test.StrContains(t, result.Content[0].Text, "companies")
}
