package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"time"
)

// The protocol revisions this server speaks.
//
// Negotiation is "echo the client's version when it is one we know, otherwise
// answer with ours and let the client decide whether it can proceed", which is
// what the specification asks for. The list is descending so [negotiateVersion]
// can prefer the newest.
const (
	protocolVersion20260728 = "2026-07-28"
	protocolVersion20251125 = "2025-11-25"
	protocolVersion20250618 = "2025-06-18"
	protocolVersion20250326 = "2025-03-26"
	protocolVersion20241105 = "2024-11-05"

	// defaultProtocolVersion is what an unrecognised client version is answered
	// with. 2025-06-18 rather than the newest: it is the revision that removed
	// JSON-RPC batching and settled the tool-result shape this server emits, so
	// it is the oldest revision whose behaviour matches what is implemented here.
	defaultProtocolVersion = protocolVersion20250618
)

// supportedProtocolVersions is in descending preference order.
var supportedProtocolVersions = []string{
	protocolVersion20260728,
	protocolVersion20251125,
	protocolVersion20250618,
	protocolVersion20250326,
	protocolVersion20241105,
}

// negotiateVersion picks the protocol revision to answer an initialize with.
func negotiateVersion(clientVersion string) string {
	if slices.Contains(supportedProtocolVersions, clientVersion) {
		return clientVersion
	}

	return defaultProtocolVersion
}

// Server answers MCP requests over a stdio transport.
//
// It holds no mutable state. Every tool is a read against the builtin registry,
// the embedded enrichment table, or a live crawl, so concurrent calls do not
// interact and there is nothing to lock.
type Server struct {
	// Name and Version identify this server to the client.
	Name    string
	Version string

	// Catalog is the set of job boards to search. Required.
	Catalog Catalog

	// Employers is the reviewed employer table. Optional: when nil, the
	// employer tool reports that no table is loaded rather than failing, so the
	// server still starts if the embedded table cannot be read.
	Employers Employers

	// Limits bounds what one tool call may cost.
	Limits Limits

	// Logger receives diagnostics. Every line goes to stderr: stdout is the
	// JSON-RPC stream and a stray log line there corrupts the session. Optional.
	Logger *slog.Logger
}

// Limits bounds the cost of a single tool call.
//
// These are the entire reason this server can exist without a posting store. See
// the package doc for the measurements behind the defaults.
type Limits struct {
	// MaxSources is the largest number of job boards one search may fetch.
	// A search selecting more than this is refused rather than run.
	MaxSources int

	// Timeout is the wall-clock budget for one search. Exceeding it truncates
	// the answer and marks it incomplete; it never turns into a silent zero.
	Timeout time.Duration

	// Fetch concurrency is deliberately not here. It belongs to the [Catalog],
	// which is the only thing that fetches; a copy on this struct would be a
	// number that looks authoritative and changes nothing. See
	// [DefaultConcurrency] for the value the binary passes to its catalog.

	// DefaultLimit and MaxLimit bound how many postings one call returns, so a
	// company with four thousand open roles cannot flood a context window.
	DefaultLimit int
	MaxLimit     int
}

// The default limits.
//
// MaxSources is 200 and Timeout is 60s against measurements taken on
// 2026-07-29 at Concurrency 8: 1 source in 0.30s, 2 in 0.74s, 24 in 2.55s and
// 121 in 14.86s, or roughly 0.12s per source once the pool is full. 200 sources
// therefore projects to about 25 seconds, leaving the 60s budget better than
// two-to-one headroom for a selection that happens to land entirely on one
// backend, where httpx's per-host cap of 4 serialises what the pool would
// otherwise overlap.
//
// DefaultConcurrency is 8, not the crawler's own default of 64. A background
// crawl with a 60-minute budget and an interactive tool call answering one
// question are not entitled to the same share of a shared job board, and the
// measurements above show 8 is already enough to answer a well-formed query in
// seconds. It is passed to the [Catalog], which is what fetches.
const (
	DefaultMaxSources   = 200
	DefaultTimeout      = 60 * time.Second
	DefaultConcurrency  = 8
	DefaultPostingLimit = 50
	MaxPostingLimit     = 500
)

// withDefaults returns the limits with any unset field filled in.
func (l Limits) withDefaults() Limits {
	if l.MaxSources <= 0 {
		l.MaxSources = DefaultMaxSources
	}

	if l.Timeout <= 0 {
		l.Timeout = DefaultTimeout
	}

	if l.MaxLimit <= 0 {
		l.MaxLimit = MaxPostingLimit
	}

	if l.DefaultLimit <= 0 {
		l.DefaultLimit = DefaultPostingLimit
	}

	if l.DefaultLimit > l.MaxLimit {
		l.DefaultLimit = l.MaxLimit
	}

	return l
}

// logger returns the configured logger, or one that discards.
func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}

	return slog.New(slog.DiscardHandler)
}

// Run serves MCP over the given streams until in is exhausted.
//
// The context bounds the work each tool call may do; cancelling it does not by
// itself close the session, because a client that keeps stdin open is entitled
// to a reply saying the server is shutting down rather than a truncated stream.
func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	return serve(in, out, func(req request) (any, *rpcError) {
		return s.handle(ctx, req)
	})
}

// handle dispatches one JSON-RPC request.
func (s *Server) handle(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req)

	case "notifications/initialized", "notifications/cancelled":
		// Nothing to do, and nothing may be written: these are notifications.
		return nil, nil

	case "ping":
		// The specification defines the result as an empty object.
		return struct{}{}, nil

	case "tools/list":
		return toolListResult{Tools: s.tools()}, nil

	case "tools/call":
		return s.callTool(ctx, req.Params)

	default:
		return nil, errorf(codeMethodNotFound, "unsupported method %q", req.Method)
	}
}

// initializeParams is the subset of the initialize request this server reads.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

// implementation names a party to the session.
type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// serverCapabilities advertises what this server can do. Tools only: there are
// no resources, prompts, sampling or logging surfaces here yet.
type serverCapabilities struct {
	Tools *toolsCapability `json:"tools,omitempty"`
}

// toolsCapability is empty because the tool list never changes at runtime, so
// there is no listChanged notification to advertise.
type toolsCapability struct{}

// initializeResult answers the initialize request.
type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      implementation     `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

// instructions is shown to the model once, at session start.
//
// It states the cost model rather than listing the tools, because the tool
// descriptions already list themselves and the one thing a model cannot discover
// by reading them is why search_jobs insists on a company.
const instructions = `This server searches live job postings by crawling company job boards on demand. There is no cached corpus: every search fetches from the ATS in real time.

Only the "companies" argument makes a search cheap. It narrows which job boards are fetched before any request is made. Every other filter (title, location, remote, pay, department, employment type, workplace type, posted_since) is applied to postings after they are fetched, so it costs nothing to add but saves nothing either.

So: search_jobs always requires "companies", and refuses when the term selects more job boards than its budget. If you do not know which companies to name, call list_companies first to find them, then search. Do not retry a refused search unchanged.`

// initialize answers the handshake.
func (s *Server) initialize(req request) (any, *rpcError) {
	var params initializeParams

	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, errorf(codeInvalidParams, "invalid initialize params: %v", err)
		}
	}

	name := s.Name
	if name == "" {
		name = "job-hunter-toolkit"
	}

	s.logger().Debug("mcp initialize",
		slog.String("client_protocol_version", params.ProtocolVersion))

	return initializeResult{
		ProtocolVersion: negotiateVersion(params.ProtocolVersion),
		Capabilities:    serverCapabilities{Tools: &toolsCapability{}},
		ServerInfo:      implementation{Name: name, Version: s.Version},
		Instructions:    instructions,
	}, nil
}
