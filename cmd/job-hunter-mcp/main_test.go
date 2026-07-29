package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// These tests drive the real binary's wiring — the builtin registry, the
// embedded employer table, the flag parsing — over an in-memory stdio session.
// None of them fetch: every request below is answered from data compiled into
// the binary, which is exactly what makes them safe to run in CI.

// runSession feeds messages to the program on stdin and returns what it wrote to
// stdout.
func runSession(t *testing.T, args []string, messages ...string) []json.RawMessage {
	t.Helper()

	var stdout, stderr strings.Builder

	err := run(args, strings.NewReader(strings.Join(messages, "\n")), &stdout, &stderr)
	must.NoError(t, err, must.Sprintf("stderr: %s", stderr.String()))

	var (
		decoder   = json.NewDecoder(strings.NewReader(stdout.String()))
		responses []json.RawMessage
	)

	for decoder.More() {
		var raw json.RawMessage

		must.NoError(t, decoder.Decode(&raw))

		responses = append(responses, raw)
	}

	return responses
}

func TestServerInitializesAndListsTools(t *testing.T) {
	t.Parallel()

	responses := runSession(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	must.Len(t, 2, responses)

	var listed struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}

	must.NoError(t, json.Unmarshal(responses[1], &listed))
	must.Len(t, 4, listed.Result.Tools)

	// The description quotes the real registry size, so this also proves the
	// builtin sources were actually loaded.
	test.StrContains(t, listed.Result.Tools[0].Description, "job boards in the registry")
}

func TestServerRefusesAnUnboundedSearchWithoutFetching(t *testing.T) {
	t.Parallel()

	// The most important behaviour of the shipped binary: asked to search
	// everything, it declines immediately rather than starting a 15-minute crawl.
	responses := runSession(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_jobs","arguments":{}}}`)

	must.Len(t, 1, responses)

	var called struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct{} `json:"error"`
	}

	must.NoError(t, json.Unmarshal(responses[0], &called))

	test.Nil(t, called.Error, test.Sprint("a refusal must arrive as a tool result"))
	test.True(t, called.Result.IsError)
	must.SliceNotEmpty(t, called.Result.Content)
	test.StrContains(t, called.Result.Content[0].Text, "companies")
}

func TestServerListsRealPlatforms(t *testing.T) {
	t.Parallel()

	responses := runSession(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_platforms","arguments":{}}}`)

	must.Len(t, 1, responses)

	var called struct {
		Result struct {
			StructuredContent struct {
				Platforms []struct {
					Platform  string `json:"platform"`
					Sources   int    `json:"sources"`
					Companies int    `json:"companies"`
				} `json:"platforms"`
				TotalSources int `json:"total_sources"`
			} `json:"structuredContent"`
		} `json:"result"`
	}

	must.NoError(t, json.Unmarshal(responses[0], &called))

	platforms := called.Result.StructuredContent.Platforms

	must.SliceNotEmpty(t, platforms)
	test.Greater(t, 0, called.Result.StructuredContent.TotalSources)

	for _, platform := range platforms {
		test.NotEq(t, "", platform.Platform)
		test.Greater(t, 0, platform.Sources)
		test.GreaterEq(t, platform.Companies, platform.Sources,
			test.Sprintf("%q reports more companies than sources", platform.Platform))
	}
}

func TestServerLooksUpARealEmployer(t *testing.T) {
	t.Parallel()

	// Exercises the embedded enrichment table end to end. The assertion is
	// deliberately about shape rather than about any particular company's facts:
	// the table is a reviewed artifact that changes, and a test that pinned a
	// headcount would fail the next time someone refreshed it.
	responses := runSession(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"lookup_employer","arguments":{"companies":["anthropic"]}}}`)

	must.Len(t, 1, responses)

	var called struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Employers []struct {
					Source struct {
						Platform string `json:"platform"`
						Key      string `json:"key"`
					} `json:"source"`
					Known bool `json:"known"`
				} `json:"employers"`
				Matched   int `json:"matched"`
				TableRows int `json:"table_rows"`
			} `json:"structuredContent"`
		} `json:"result"`
	}

	must.NoError(t, json.Unmarshal(responses[0], &called))

	test.False(t, called.Result.IsError)

	content := called.Result.StructuredContent

	must.SliceNotEmpty(t, content.Employers)
	test.Greater(t, 0, content.Matched)
	test.Eq(t, "greenhouse", content.Employers[0].Source.Platform)
	test.Eq(t, "anthropic", content.Employers[0].Source.Key)

	// An unresolved company is a valid answer, so the row count is what tells a
	// caller whether the table loaded at all.
	test.Greater(t, 0, content.TableRows, test.Sprint("the embedded employer table should not be empty"))
}

func TestServerRejectsABadLogLevel(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder

	err := run([]string{"--log-level", "chatty"}, strings.NewReader(""), &stdout, &stderr)

	must.Error(t, err)
	test.StrContains(t, err.Error(), "invalid --log-level")
}

func TestServerHonoursTheMaxSourcesFlag(t *testing.T) {
	t.Parallel()

	// A max of 1 makes even a modest term over-broad, which proves the flag
	// reaches the bound rather than being parsed and dropped.
	responses := runSession(t, []string{"--max-sources", "1"},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_jobs","arguments":{"companies":["tech"]}}}`)

	must.Len(t, 1, responses)

	var called struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}

	must.NoError(t, json.Unmarshal(responses[0], &called))

	test.True(t, called.Result.IsError)
	must.SliceNotEmpty(t, called.Result.Content)
	test.StrContains(t, called.Result.Content[0].Text, "more than the 1")
}

func TestServerWritesNothingButProtocolToStdout(t *testing.T) {
	t.Parallel()

	// Structured data goes to stdout, diagnostics to stderr. A stray log line on
	// stdout corrupts the session, so this asserts stdout parses cleanly as
	// JSON-RPC even at debug verbosity.
	var stdout, stderr strings.Builder

	err := run([]string{"--log-level", "debug"},
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), &stdout, &stderr)
	must.NoError(t, err)

	decoder := json.NewDecoder(strings.NewReader(stdout.String()))

	for decoder.More() {
		var raw map[string]any

		must.NoError(t, decoder.Decode(&raw),
			must.Sprintf("stdout is not clean JSON-RPC: %q", stdout.String()))
		test.Eq(t, "2.0", raw["jsonrpc"])
	}

	// The debug logs must have gone somewhere, and that somewhere is stderr.
	test.StrContains(t, stderr.String(), "job-hunter-mcp ready")
}

func TestVersionFlagExitsWithoutServing(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder

	must.NoError(t, run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr))

	test.Eq(t, "", stdout.String(), test.Sprint("stdout is the protocol stream and must stay empty"))
	test.StrContains(t, stderr.String(), version)
}
