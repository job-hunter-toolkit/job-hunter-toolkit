package mcp

import (
	"context"
	"net/http"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// BuiltinCatalog is a [Catalog] over the job boards compiled into this binary,
// fetched live at call time.
//
// It is the only part of this package that knows the crawler exists. Everything
// above it — the tool schemas, the bound, the refusals, the sorting — is written
// against [Catalog], which is what lets the entire tool surface be tested
// without a network.
type BuiltinCatalog struct {
	client      *http.Client
	concurrency int

	// sources is the registry snapshot this catalog serves, and index maps a
	// source back to the fetch function that was registered with it.
	//
	// Held rather than re-derived per call because [services.SourcesMatching]
	// walks and interleaves the whole registry each time, and the tool layer
	// calls it more than once per request.
	sources []Source
	index   map[jobposting.PostingSource]services.Source
}

// NewBuiltinCatalog returns a catalog over every builtin job source, fetching
// with the given client and at most concurrency sources at once.
func NewBuiltinCatalog(client *http.Client, concurrency int) *BuiltinCatalog {
	if concurrency < 1 {
		concurrency = 1
	}

	// SourcesMatching(nil) returns the whole registry, already interleaved by
	// platform so a crawl does not open with every request aimed at one backend.
	registered := services.SourcesMatching(nil)

	catalog := &BuiltinCatalog{
		client:      client,
		concurrency: concurrency,
		sources:     make([]Source, 0, len(registered)),
		index:       make(map[jobposting.PostingSource]services.Source, len(registered)),
	}

	for _, source := range registered {
		catalog.sources = append(catalog.sources, Source{
			Platform: source.Platform,
			Key:      source.Key,
			Company:  source.Company,
		})

		catalog.index[jobposting.PostingSource{Platform: source.Platform, Key: source.Key}] = source
	}

	return catalog
}

// Sources returns every builtin job board.
func (c *BuiltinCatalog) Sources() []Source {
	return c.sources
}

// Select narrows the registry by company term, exactly as `--company` does on
// the CLI. This is the narrowing that makes a bounded search possible: it
// chooses which boards are fetched, before any request is made.
func (c *BuiltinCatalog) Select(terms []string) []Source {
	matched := services.SourcesMatching(terms)

	selected := make([]Source, 0, len(matched))

	for _, source := range matched {
		selected = append(selected, Source{
			Platform: source.Platform,
			Key:      source.Key,
			Company:  source.Company,
		})
	}

	return selected
}

// Crawl fetches postings from the given boards.
//
// Sources with no registered fetch function are skipped rather than reported.
// The only way to hold one is to have constructed a [Source] by hand and passed
// it back in, which the tool layer never does: every source it can reach came
// out of [BuiltinCatalog.Select].
func (c *BuiltinCatalog) Crawl(ctx context.Context, sources []Source) jobposting.Seq {
	funcs := make([]internal.JobsFunc, 0, len(sources))

	for _, source := range sources {
		if registered, ok := c.index[source.Posting()]; ok {
			funcs = append(funcs, registered.Jobs)
		}
	}

	return internal.AllWithConcurrency(ctx, c.client, c.concurrency, funcs...)
}
