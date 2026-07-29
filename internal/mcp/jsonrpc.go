package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// The JSON-RPC 2.0 error codes this server uses.
//
// The split between these and a tool error matters more than it looks. A code
// here means the *call* was malformed: no such method, unparseable arguments.
// A tool that ran and declined — because the query was unbounded, or matched no
// company — returns a normal result with IsError set, because that is what the
// model on the other end actually reads. Reporting a refusal as codeInvalidParams
// hides the sentence explaining which argument to narrow behind a transport
// error most clients surface as a stack trace.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// request is one incoming JSON-RPC 2.0 message.
//
// ID is json.RawMessage because JSON-RPC permits a string or a number and
// requires the response to echo the value unchanged; decoding into any would
// turn the integer 1 into a float64 and send back "1e+00" for ids near the top
// of the int64 range. A request with no ID is a notification and must not be
// answered at all.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message expects no response. JSON-RPC
// distinguishes an absent id from a null one, and encoding/json gives us that:
// an absent id decodes to a nil RawMessage.
func (r request) isNotification() bool {
	return len(r.ID) == 0
}

// response is one outgoing JSON-RPC 2.0 message. Exactly one of Result and
// Error is set.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the error object of a JSON-RPC 2.0 response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// errorf builds an rpcError with a formatted message.
func errorf(code int, format string, args ...any) *rpcError {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// nullID is the id echoed when a request could not be parsed well enough to
// recover its own id, which JSON-RPC 2.0 requires be null rather than omitted.
var nullID = json.RawMessage("null")

// isMalformedJSON reports whether err means the bytes were not valid JSON, as
// opposed to the stream having failed underneath the decoder. Only the first
// kind is worth answering with a parse error.
func isMalformedJSON(err error) bool {
	var (
		syntax    *json.SyntaxError
		unmarshal *json.UnmarshalTypeError
	)

	return errors.As(err, &syntax) || errors.As(err, &unmarshal)
}

// serve reads newline-delimited JSON-RPC messages from r and writes responses to
// w until r is exhausted or the handler asks to stop.
//
// # Why this is hand-written
//
// github.com/modelcontextprotocol/go-sdk v1.7.0 exists and is the official
// implementation. It was measured and declined on 2026-07-29, for two reasons
// that are specific to this repository rather than general:
//
//  1. Portability. Its module graph is 8 direct and 3 indirect requirements —
//     golang-jwt, google/jsonschema-go, segmentio/encoding, yosida95/uritemplate,
//     x/oauth2, x/time, x/tools, go-cmp, segmentio/asm, x/sync, x/sys. That takes
//     this module from 16 entries to 27. segmentio/asm is hand-written
//     architecture-specific assembly, and CI asserts this tree builds for
//     GOOS=js/wasm and GOOS=wasip1. Taking a dependency with per-architecture
//     assembly into a repository whose first invariant is four-OS/arch
//     portability is a liability with no upside here.
//
//  2. None of it is reachable. The jwt and oauth2 requirements serve
//     authenticated HTTP transports; uritemplate serves resource templates;
//     x/tools serves the schema inference this file does not use because the
//     schemas are written out literally in tools.go. A stdio server uses none of
//     them.
//
// What is left to implement is small: MCP's stdio transport is newline-delimited
// JSON-RPC 2.0, and JSON-RPC 2.0 batching was removed in protocol revision
// 2025-06-18, so a message is one JSON value. That is this file, and it is
// shorter than the go.sum diff would have been.
//
// If this server ever needs sampling, elicitation, resource subscriptions or an
// HTTP transport, the balance flips and the SDK should be adopted. Serving four
// read-only tools over stdio is not that.
func serve(r io.Reader, w io.Writer, handle func(request) (any, *rpcError)) error {
	var (
		decoder = json.NewDecoder(r)
		encoder = json.NewEncoder(w)
	)

	for {
		var req request

		if err := decoder.Decode(&req); err != nil {
			// EOF is how an MCP stdio session ends: the client closed stdin.
			// ErrUnexpectedEOF means it closed mid-message, which is the same
			// outcome for us and equally not worth reporting as a failure.
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}

			if !isMalformedJSON(err) {
				// A broken pipe rather than a bad message. There is nothing to
				// write the complaint to.
				return err
			}

			// A malformed message leaves the decoder stopped mid-value with no
			// way to find where the next one begins, so report it and stop
			// rather than spin on the same bytes forever.
			if err := encoder.Encode(response{
				JSONRPC: "2.0",
				ID:      nullID,
				Error:   errorf(codeParseError, "parse error: %v", err),
			}); err != nil {
				return err
			}

			return nil
		}

		if req.JSONRPC != "2.0" {
			if req.isNotification() {
				continue
			}

			if err := encoder.Encode(response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   errorf(codeInvalidRequest, "unsupported jsonrpc version %q, want \"2.0\"", req.JSONRPC),
			}); err != nil {
				return err
			}

			continue
		}

		result, rpcErr := handle(req)

		// A notification gets no reply even when handling it failed. This is not
		// a courtesy: a client that sent "notifications/initialized" and receives
		// a response for it sees a reply to an id it never issued.
		if req.isNotification() {
			continue
		}

		out := response{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			out.Error = rpcErr
		} else {
			out.Result = result
		}

		if err := encoder.Encode(out); err != nil {
			return err
		}
	}
}
