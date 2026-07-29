package mcp

import (
	"context"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// Source is one crawlable job board: one company on one ATS.
//
// It is a copy of the identifying fields of services.Source rather than that
// type itself, deliberately. The tool layer needs to name, count, group and sort
// sources; it never needs to fetch one. Keeping the fetch function out of this
// struct is what lets [Catalog] be implemented by a fixture in a test with no
// HTTP client anywhere in the process.
type Source struct {
	// Platform is the ATS family: "greenhouse", "lever", "workday".
	Platform string `json:"platform"`

	// Key is the tenant identifier the adapter fetches with: a board slug on
	// most platforms, a tenant URL on Workday, a hostname on Phenom.
	Key string `json:"key"`

	// Company is the human-facing name derived from Key. It is not the identity
	// — several tenants can map to one employer, and one Workday tenant can host
	// several brands — so nothing joins on it.
	Company string `json:"company"`
}

// Posting is the join key docs/architecture-roadmap.md settles on: platform plus
// tenant key. It is what [Employers] looks up by.
func (s Source) Posting() jobposting.PostingSource {
	return jobposting.PostingSource{Platform: s.Platform, Key: s.Key}
}

// Catalog is the set of job boards a server can reach, and the ability to fetch
// a chosen subset of them.
//
// Selecting and crawling are separate methods because the whole cost model of
// this server depends on being able to count what a query would fetch before
// fetching it. A single Search(query) method could not refuse an overbroad
// request without first paying for it.
type Catalog interface {
	// Sources returns every known source in a deterministic order.
	Sources() []Source

	// Select returns the sources whose company name or tenant key contains any
	// of the given terms, case-insensitively. With no usable terms it returns
	// every source, matching services.SourcesMatching; callers that must not
	// crawl everything are responsible for checking first.
	Select(terms []string) []Source

	// Crawl fetches postings from exactly the given sources. It must stop when
	// the context is done, yielding whatever it has.
	Crawl(ctx context.Context, sources []Source) jobposting.Seq
}

// Employers answers what is known about the employer behind a source, from the
// reviewed table compiled into the binary. Nothing here makes a request.
//
// The method set is satisfied as-is by *enrich.Table, which is the point: this
// interface exists so tests can substitute a small table, not to wrap one.
type Employers interface {
	// For returns the reviewed row for a source, if one exists. A missing row
	// means nobody has resolved that company yet, which is the default and not
	// an error.
	For(source jobposting.PostingSource) (*enrich.Employer, bool)

	// Len reports how many rows the table holds, so a caller can report coverage
	// honestly rather than implying the table is complete.
	Len() int
}
