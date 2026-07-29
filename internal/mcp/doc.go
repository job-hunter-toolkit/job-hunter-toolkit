// Package mcp serves this project's crawl over the Model Context Protocol, so
// an agent can search job postings, enumerate coverage, and look up an employer
// without shelling out to the CLI and parsing its output.
//
// # The bound is the design
//
// An agent tool call cannot spend fifteen minutes crawling. A full crawl of the
// builtin registry measured 877 seconds over 8,235 sources on 2026-07-28
// (docs/measurements/2026-07-28-crawl.md), and there is no posting store yet —
// docs/posting-cache.md is a design proposal, explicitly unimplemented. That
// leaves exactly one honest way to answer a tool call quickly, and it is the one
// the CLI already uses: narrow the *sources* before fetching, rather than
// crawling everything and filtering the stream.
//
// Only company terms narrow sources. Every other filter in [query.Query] —
// title, location, pay, department, workplace type — is applied downstream of
// the crawl and therefore costs a full crawl to evaluate. So [SearchJobs]
// requires company terms and refuses without them, and refuses again when the
// terms select more sources than its budget allows. Measured on this machine at
// concurrency 8: 1 source in 0.30s, 2 in 0.74s, 24 in 2.55s, 121 in 14.86s.
//
// Refusing is the feature. A tool that hung for fifteen minutes, or that quietly
// answered "no remote Go jobs exist" from a crawl that timed out halfway, would
// be worse than one that says which argument to narrow. Refusals are returned as
// tool errors carrying that instruction, not as protocol errors, so the model
// reading the result can correct the call itself.
//
// # Partial answers say so
//
// When the deadline expires mid-crawl the postings already collected are
// returned with complete=false and a reason, never silently as a whole answer.
// This is the same rule the crawler applies to its manifests: a partial crawl is
// never reported as complete.
//
// # No SDK
//
// The protocol is implemented directly on encoding/json rather than with
// github.com/modelcontextprotocol/go-sdk. See jsonrpc.go for the measurement
// that decided it.
package mcp
