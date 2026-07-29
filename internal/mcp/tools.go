package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// schema describes one tool argument.
//
// Written out by hand rather than inferred by reflection from a Go struct. The
// descriptions are the entire user interface here — an agent picks and fills a
// tool from this text and nothing else — so they are the part of this package
// most worth reading and least worth generating.
//
// It deliberately cannot describe a nested object. No argument here is one, and
// keeping the capability out is what lets [objectSchema] below guarantee it
// always publishes a properties map.
type schema struct {
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Items       *schema  `json:"items,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
}

// objectSchema is a tool's whole input schema.
//
// Properties carries no omitempty on purpose. MCP clients expect an input schema
// to be an object with a properties map, and a tool that happens to take no
// arguments would otherwise publish a bare {"type":"object"} — valid JSON Schema,
// but a shape several clients mishandle. [object] guarantees the map is non-nil
// so it serializes as {} rather than null.
type objectSchema struct {
	Type                 string             `json:"type"`
	Properties           map[string]*schema `json:"properties"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *bool              `json:"additionalProperties,omitempty"`
}

// stringList describes an array-of-strings argument.
func stringList(description string) *schema {
	return &schema{
		Type:        "array",
		Description: description,
		Items:       &schema{Type: "string"},
	}
}

// enumList describes an array argument whose entries come from a closed set.
func enumList(description string, values []string) *schema {
	return &schema{
		Type:        "array",
		Description: description,
		Items:       &schema{Type: "string", Enum: values},
	}
}

func boolArg(description string) *schema {
	return &schema{Type: "boolean", Description: description}
}

func intArg(description string, min, max float64) *schema {
	return &schema{Type: "integer", Description: description, Minimum: &min, Maximum: &max}
}

// object builds a tool's input schema. A nil properties map becomes an empty
// one, so the published schema always has a properties key.
func object(properties map[string]*schema, required ...string) *objectSchema {
	if properties == nil {
		properties = map[string]*schema{}
	}

	return &objectSchema{
		Type:                 "object",
		Properties:           properties,
		Required:             required,
		AdditionalProperties: ptr(false),
	}
}

// annotations are the behavioural hints MCP defines for a tool. All four tools
// here are read-only; the two that crawl are open-world because they reach
// third-party job boards whose contents this project does not control.
type annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint,omitempty"`
	IdempotentHint  bool   `json:"idempotentHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// tool is one entry of tools/list.
//
// OutputSchema is deliberately not declared. The specification requires that a
// tool declaring one always return structuredContent validating against it, and
// these results embed [jobposting.JobPosting], whose shape is owned by another
// package and documented as free to grow. A hand-maintained copy of that schema
// would drift, and a drifted output schema turns a correct answer into a client
// -side validation failure. structuredContent is still returned; it is simply
// not promised in advance.
type tool struct {
	Name        string        `json:"name"`
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description"`
	InputSchema *objectSchema `json:"inputSchema"`
	Annotations annotations   `json:"annotations,omitzero"`
}

// toolListResult is the tools/list response.
type toolListResult struct {
	Tools []tool `json:"tools"`
}

// textContent is a text block of a tool result.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolResult is the tools/call response.
type toolResult struct {
	Content           []textContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

// callToolParams is the tools/call request.
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// refuse returns a tool error: the call was well-formed but this server will not
// answer it as asked.
//
// It is a result rather than a JSON-RPC error on purpose — see the comment on
// the error codes in jsonrpc.go. The message is written to be acted on: it says
// what was wrong and which argument to change, because the model reading it is
// the only thing that can fix the call.
func refuse(format string, args ...any) (any, *rpcError) {
	return toolResult{
		Content: []textContent{{Type: "text", Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}, nil
}

// succeed returns a tool result carrying a structured payload.
//
// The payload is also serialized into the text block. That is redundant for a
// client that reads structuredContent, and it is what the specification asks for
// so that a client which does not read it still receives the answer rather than
// an empty result.
func succeed(payload any) (any, *rpcError) {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, errorf(codeInternalError, "encode result: %v", err)
	}

	return toolResult{
		Content:           []textContent{{Type: "text", Text: string(encoded)}},
		StructuredContent: payload,
	}, nil
}

// callTool routes a tools/call request to its handler.
func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params callToolParams

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, errorf(codeInvalidParams, "invalid tools/call params: %v", err)
		}
	}

	switch params.Name {
	case "search_jobs":
		return s.searchJobs(ctx, params.Arguments)
	case "list_companies":
		return s.listCompanies(params.Arguments)
	case "list_platforms":
		return s.listPlatforms(params.Arguments)
	case "lookup_employer":
		return s.lookupEmployer(params.Arguments)
	}

	// An unknown tool name is a malformed call, not a tool that ran and failed,
	// so this is a protocol error.
	return nil, errorf(codeInvalidParams, "unknown tool %q; available tools: %s",
		params.Name, strings.Join(s.toolNames(), ", "))
}

// toolNames returns the tool names in the order tools/list reports them.
func (s *Server) toolNames() []string {
	tools := s.tools()
	names := make([]string, 0, len(tools))

	for _, t := range tools {
		names = append(names, t.Name)
	}

	return names
}

// decodeArgs unmarshals tool arguments, rejecting unknown fields.
//
// Strict decoding is worth the friction here. An agent that invents
// `"company": "anthropic"` instead of `"companies": ["anthropic"]` would
// otherwise have its argument silently dropped and receive a crawl of every
// source, refused for being unbounded, with nothing pointing at the typo.
func decodeArgs(raw json.RawMessage, into any) *rpcError {
	if len(raw) == 0 {
		return nil
	}

	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(into); err != nil {
		return errorf(codeInvalidParams, "invalid arguments: %v", err)
	}

	return nil
}

// ptr returns a pointer to v.
//
// The annotation and schema fields that use it are pointers because absent and
// false mean different things to a client: an omitted openWorldHint is "not
// stated", while false is "this tool does not touch the outside world".
func ptr[T any](v T) *T { return &v }
