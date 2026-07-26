package companies

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	jobpostings "github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

// DirectPlatform is the platform name the adapters in this package register
// under, and the value that reaches [jobpostings.PostingSource.Platform].
//
// They are not an ATS family: each one talks to a single employer's own careers
// site, so there is no shared vendor to name. Grouping them under one name still
// matters, because the crawl interleaves work by platform to keep one backend
// from consuming every worker.
const DirectPlatform = "direct"

// oxideCompany is the identifier this employer is crawled and filtered by.
//
// Lower case to match every other source in the registry: the company list is
// sorted case-insensitively but printed verbatim, and `--company oxide` is
// compared against this string.
const oxideCompany = "oxide"

// Sources describes the direct-employer adapters in this package so that
// package services can register them into the crawl.
//
// It exists because those adapters were never in the crawl at all. A crawl is
// assembled from services.Builtin, every entry of which is added by an init in
// package services, and `grep -rn "internal/companies"` matched nothing outside
// this package's own tests: Oxide and Uber have been maintained, tested, and
// completely unreachable from the CLI. Both were dead code in the binary.
//
// The registration itself has to happen inside package services, because
// registerBuiltin is unexported there and this package cannot import it without
// creating an import cycle. So the naming and the adapters stay here, where they
// belong, and the services side is a single call.
func Sources() []Source {
	return []Source{
		{Key: oxideCompany, Company: oxideCompany, Jobs: Oxide},
		{Key: uberCompany, Company: uberCompany, Jobs: Uber},
	}
}

// Source is one direct-employer adapter: how it is keyed, how it is named, and
// how it fetches.
//
// It mirrors the fields services.Source needs rather than being that type,
// which would be an import cycle.
type Source struct {
	// Key identifies the employer to its own careers site, and Company is the
	// name a person would recognise. They are the same string for every adapter
	// here, and are kept separate anyway because the registry distinguishes
	// them: conflating the two once put raw tenant URLs into the user-facing
	// company list and made `--company` silently match nothing.
	Key     string
	Company string

	// Jobs fetches this employer's postings.
	Jobs jobpostings.JobsFunc
}

// Oxide returns a jobpostings.Jobs function for Oxide Computing.
// It scrapes the Oxide careers page at "https://oxide.computer/careers"
// and yields all discovered job postings.
func Oxide(ctx context.Context, httpClient *http.Client) jobpostings.Jobs {
	return func(yield func(*jobpostings.JobPosting, error) bool) {
		baseURLStr := "https://oxide.computer/careers"
		baseURL, err := url.Parse(baseURLStr)
		if err != nil {
			yield(nil, fmt.Errorf("invalid Oxide URL: %w", err))
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURLStr, nil)
		if err != nil {
			yield(nil, fmt.Errorf("creating request failed: %w", err))
			return
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			yield(nil, fmt.Errorf("fetching Oxide careers page failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			yield(nil, fmt.Errorf("unexpected status code %d for Oxide careers page", resp.StatusCode))
			return
		}

		doc, err := html.Parse(resp.Body)
		if err != nil {
			yield(nil, fmt.Errorf("parsing Oxide HTML failed: %w", err))
			return
		}

		// Oxide lists every opening on a single page, so there is no pagination
		// to follow.

		// Find all job listing links.
		jobLinks := findOxideJobLinks(doc)
		for _, a := range jobLinks {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			var jp jobpostings.JobPosting
			// Resolve the href attribute.
			for _, attr := range a.Attr {
				if attr.Key == "href" && strings.HasPrefix(attr.Val, "/careers/") && !strings.Contains(attr.Val, "feed") {
					relative, err := url.Parse(attr.Val)
					if err != nil {
						yield(nil, fmt.Errorf("parsing relative URL failed: %w", err))
						return
					}
					jp.URL = baseURL.ResolveReference(relative).String()
					break
				}
			}
			// Attempt to extract the job title from a descendant <h3> element.
			if h3 := findFirstElementByTag(a, "h3"); h3 != nil {
				jp.Title = strings.TrimSpace(getText(h3))
			} else {
				// Fallback: use the text of the link itself.
				jp.Title = strings.TrimSpace(getText(a))
			}
			// Extract the location from the last <div> child of the link.
			jp.Location = extractLastDivText(a)
			// Set the company field.
			jp.Company = oxideCompany
			jp.Source = jobpostings.PostingSource{Platform: DirectPlatform, Key: oxideCompany}
			if !yield(&jp, nil) {
				return
			}
		}
	}
}

// findOxideJobLinks searches the document for all <a> elements that are job listings.
// We assume that each job listing is an <a> element whose parent is an <li>
// (and whose href starts with "/careers/" and does not include "feed").
func findOxideJobLinks(n *html.Node) []*html.Node {
	var links []*html.Node
	var search func(*html.Node)
	search = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if n.Parent != nil && n.Parent.Data == "li" {
				for _, attr := range n.Attr {
					if attr.Key == "href" && strings.HasPrefix(attr.Val, "/careers/") && !strings.Contains(attr.Val, "feed") {
						links = append(links, n)
						break
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			search(c)
		}
	}
	search(n)
	return links
}

// findFirstElementByTag returns the first descendant element with the given tag.
func findFirstElementByTag(n *html.Node, tag string) *html.Node {
	var result *html.Node
	var search func(*html.Node)
	search = func(n *html.Node) {
		if result != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == tag {
			result = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			search(c)
		}
	}
	search(n)
	return result
}

// extractLastDivText returns the trimmed text content of the last descendant <div> of node n.
func extractLastDivText(n *html.Node) string {
	var lastDiv *html.Node
	var search func(*html.Node)
	search = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			lastDiv = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			search(c)
		}
	}
	search(n)
	if lastDiv != nil {
		return strings.TrimSpace(getText(lastDiv))
	}
	return ""
}

// getText recursively extracts and concatenates text from node n.
func getText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var builder strings.Builder
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return builder.String()
}
