package internal

import (
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/query"
)

// The filter vocabulary now lives in the public
// [github.com/job-hunter-toolkit/job-hunter-toolkit/query] package, so a TUI, an
// MCP tool and a browser build can share one query language instead of three.
// What is left here is an alias and a forward; see the note in job_posting.go.

// Filter is [query.Query]. The public name is Query — the package is the query
// language and the type is a query — but the alias keeps every existing caller,
// including TestFilterFieldsAreWiredIn's reflection over the struct, compiling
// unchanged.
type Filter = query.Query

// Dedupe calls [jobposting.Dedupe].
func Dedupe(jobs Jobs) Jobs { return jobposting.Dedupe(jobs) }
