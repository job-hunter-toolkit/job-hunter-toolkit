package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/enrich"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// fakeCatalog is a [Catalog] over a fixed set of sources and postings, with no
// network anywhere.
//
// The entire point of [Catalog] being an interface is that this type can exist:
// every bound, refusal, sort order and summary field in this package is testable
// without a job board, an HTTP client, or a recorded transcript.
type fakeCatalog struct {
	sources  []Source
	postings map[string][]*jobposting.JobPosting

	// failing names sources that yield an error instead of postings.
	failing map[string]error

	// block, when set, makes Crawl wait for the context to end before yielding
	// anything further, so the deadline path can be exercised.
	block bool

	// crawled records which sources Crawl was actually asked for, which is how a
	// test proves narrowing happened before fetching rather than after.
	crawled []Source

	// calls counts crawls, and drives the rotation below.
	calls int
}

// rotate reorders postings by a different offset on every crawl.
//
// The real crawler yields in whatever order boards answer, which varies run to
// run with worker scheduling and network timing. A fixture that always yielded
// in the same order would let a missing sort pass every determinism test in this
// file, so it must not.
func (c *fakeCatalog) rotate(postings []*jobposting.JobPosting) []*jobposting.JobPosting {
	if len(postings) < 2 {
		return postings
	}

	offset := c.calls % len(postings)
	rotated := make([]*jobposting.JobPosting, 0, len(postings))

	rotated = append(rotated, postings[offset:]...)
	rotated = append(rotated, postings[:offset]...)

	return rotated
}

// key is the identity used to look postings up in the fixture.
func key(s Source) string { return s.Platform + "/" + s.Key }

func (c *fakeCatalog) Sources() []Source { return c.sources }

// Select mirrors services.SourcesMatching: case-insensitive substring match
// against both the display name and the tenant key, and everything when no
// usable term is given.
func (c *fakeCatalog) Select(terms []string) []Source {
	usable := usableTerms(terms)
	if len(usable) == 0 {
		return c.sources
	}

	var matched []Source

	for _, source := range c.sources {
		for _, term := range usable {
			term = strings.ToLower(term)

			if strings.Contains(strings.ToLower(source.Company), term) ||
				strings.Contains(strings.ToLower(source.Key), term) {
				matched = append(matched, source)

				break
			}
		}
	}

	return matched
}

func (c *fakeCatalog) Crawl(ctx context.Context, sources []Source) jobposting.Seq {
	c.crawled = append(c.crawled, sources...)
	c.calls++

	// Sources are visited in a rotated order too, because the real pool starts
	// them concurrently and finishes them in no particular order.
	ordered := make([]Source, 0, len(sources))
	if len(sources) > 1 {
		offset := c.calls % len(sources)
		ordered = append(ordered, sources[offset:]...)
		ordered = append(ordered, sources[:offset]...)
	} else {
		ordered = append(ordered, sources...)
	}

	return func(yield func(*jobposting.JobPosting, error) bool) {
		for _, source := range ordered {
			if err, ok := c.failing[key(source)]; ok {
				if !yield(nil, err) {
					return
				}

				continue
			}

			for _, posting := range c.rotate(c.postings[key(source)]) {
				if !yield(posting, nil) {
					return
				}
			}
		}

		if c.block {
			// Stand in for a board that never answers. The crawl ends only
			// because its budget expired, which is exactly the case that must
			// come back marked incomplete rather than empty.
			<-ctx.Done()
		}
	}
}

// posting builds a fixture posting.
func posting(company, title, location, url string) *jobposting.JobPosting {
	return &jobposting.JobPosting{
		Company:  company,
		Title:    title,
		Location: location,
		URL:      url,
	}
}

// testCatalog is the fixture most tests use: three companies across two ATS
// platforms, one of them registered on both.
func testCatalog() *fakeCatalog {
	sources := []Source{
		{Platform: "greenhouse", Key: "acme", Company: "acme"},
		{Platform: "lever", Key: "acme-labs", Company: "Acme Labs"},
		{Platform: "greenhouse", Key: "globex", Company: "globex"},
		{Platform: "lever", Key: "initech", Company: "initech"},
	}

	return &fakeCatalog{
		sources: sources,
		postings: map[string][]*jobposting.JobPosting{
			"greenhouse/acme": {
				posting("acme", "Staff Security Engineer", "Remote - US", "https://acme.example/3"),
				posting("acme", "Accountant", "Dublin, IE", "https://acme.example/1"),
				posting("acme", "Backend Engineer", "London, UK", "https://acme.example/2"),
			},
			"lever/acme-labs": {
				posting("Acme Labs", "Research Engineer", "San Francisco, CA", "https://acmelabs.example/1"),
			},
			"greenhouse/globex": {
				posting("globex", "Sales Director", "New York, NY", "https://globex.example/1"),
			},
			"lever/initech": {
				posting("initech", "Backend Engineer", "Austin, TX", "https://initech.example/1"),
			},
		},
	}
}

// testServer returns a server over the given catalog with small, explicit
// limits so a test can reach the bound without a large fixture.
func testServer(catalog Catalog) *Server {
	return &Server{
		Name:      "job-hunter-toolkit",
		Version:   "test",
		Catalog:   catalog,
		Employers: testEmployers(),
		Limits: Limits{
			MaxSources:   3,
			Timeout:      time.Second,
			DefaultLimit: 10,
			MaxLimit:     20,
		},
	}
}

// testEmployers is a reviewed table holding a row for exactly one of the
// fixture's sources, so both the resolved and the unresolved answer are
// exercised.
func testEmployers() Employers {
	public := true

	return enrich.NewTable(&enrich.Employer{
		Source:       jobposting.PostingSource{Platform: "greenhouse", Key: "acme"},
		Company:      "acme",
		LegalName:    "Acme Corporation",
		CIK:          "0000000001",
		Ticker:       "ACME",
		Exchange:     "NYSE",
		Industry:     "7372 Prepackaged Software",
		SIC:          "7372",
		Public:       &public,
		Employees:    1234,
		Headquarters: "Springfield, USA",
		Match: enrich.Match{
			Method:      enrich.MethodManual,
			Confidence:  enrich.ConfidenceHigh,
			DataSources: []string{"sec-edgar"},
			RetrievedAt: "2026-07-01",
		},
	})
}

// errBoardRetired stands in for the ordinary failure at this scale: a board that
// no longer exists.
var errBoardRetired = errors.New("board returned 404")

// mustNotBeNil fails the test when v is nil, with a message naming what was
// expected. It exists because a nil result and a result with a nil field fail in
// very different places otherwise.
func mustNotBeNil(t *testing.T, v any, what string) {
	t.Helper()

	if v == nil {
		t.Fatalf("expected %s, got nil", what)
	}
}
