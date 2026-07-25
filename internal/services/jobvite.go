package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

func init() {
	registerBuiltin(multiJobsFunc(Jobvite, JobviteCompanies))
}

var JobviteCompanies = []string{
	"actionet",
	"aspirepublicschools",
	"ayrwellness",
	"bigbrandtire",
	"biofiredx",
	"blackbear",
	"evergreenhealth",
	"innio",
	"iowaclinic",
	"ips-careers",
	"laborie",
	"martinmarietta",
	"mercy-health",
	"ninjaone",
	"nutanix",
	"paloaltonetworks",
	"pilgrims",
	"securityfinance",
	"splunk-careers",
	"torrancememorialjobs",
	"tylertech",
	"visiongroup",
	"von",
	"washingtonhospital",
	"webmd",
	"weisiger",
	"zones",
}

// jobvitePage fetches one page of a Jobvite search and parses it.
//
// Split out so the response body is closed per page rather than accumulating one
// open body per page for the lifetime of the crawl.
func jobvitePage(ctx context.Context, httpClient *http.Client, company string, page int) (*html.Node, error) {
	// To include the job description, add ?content=true to the request.
	url := fmt.Sprintf("https://jobs.jobvite.com/%s/search?l=&c=&q=&nl=1&p=%d", company, page)

	return fetchHTML(ctx, httpClient, "Jobvite", company, url)
}

// jobviteHasAttr reports whether any node in the tree is the named element
// carrying the named attribute. Jobvite signals both "no more results" and its
// deprecated-board notice this way.
func jobviteHasAttr(n *html.Node, element, attr string) bool {
	if n.Type == html.ElementNode && n.Data == element {
		for _, a := range n.Attr {
			if a.Key == attr {
				return true
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if jobviteHasAttr(c, element, attr) {
			return true
		}
	}

	return false
}

// jobviteDeprecated reports whether the page is Jobvite's deprecated-board
// placeholder rather than a real job list.
func jobviteDeprecated(n *html.Node) bool {
	if n.Type == html.ElementNode && n.Data == "a" {
		for _, a := range n.Attr {
			if a.Key == "href" && strings.Contains(a.Val, "jobvite.com/why-jobvite") {
				return true
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if jobviteDeprecated(c) {
			return true
		}
	}

	return false
}

// jobviteJobLink extracts a posting from an anchor node, reporting false if the
// node is not a job link or the page structure does not match expectations.
func jobviteJobLink(n *html.Node, company string) (*internal.JobPosting, bool) {
	if n.Type != html.ElementNode || n.Data != "a" {
		return nil, false
	}

	var href string

	for _, a := range n.Attr {
		if a.Key == "href" && strings.Contains(a.Val, "/job/") {
			href = a.Val

			break
		}
	}

	if href == "" || n.FirstChild == nil {
		return nil, false
	}

	title := strings.TrimSpace(n.FirstChild.Data)

	// The location sits in a sibling cell of the row containing this link. The
	// path is fragile, so every hop is checked: Jobvite serves several page
	// templates, and an unexpected one must not panic a crawl of a thousand
	// companies.
	location := "unknown"

	if cell := jobviteLocationNode(n); cell != nil {
		if joined := strings.Join(strings.Fields(strings.TrimSpace(cell.Data)), " "); joined != "" {
			location = joined
		}
	}

	return &internal.JobPosting{
		Company:  company,
		URL:      "https://jobs.jobvite.com" + href,
		Title:    title,
		Location: location,
	}, true
}

// jobviteLocationNode walks from a job link to the text node holding its
// location, returning nil if the page does not have the expected shape.
func jobviteLocationNode(n *html.Node) *html.Node {
	if n.Parent == nil || n.Parent.Parent == nil {
		return nil
	}

	cur := n.Parent.Parent.FirstChild

	// Skip to the fourth child of the row, where the location cell lives.
	for range 3 {
		if cur == nil {
			return nil
		}

		cur = cur.NextSibling
	}

	if cur == nil {
		return nil
	}

	return cur.FirstChild
}

// Jobvite returns the job postings for a company hosted on Jobvite.
//
// Note that Jobvite has been migrating tenants to a client-side rendered
// template that serves no job links in its HTML. Those boards parse as empty
// rather than failing; see docs/source-backlog.md.
func Jobvite(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		for page := 1; ; page++ {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			doc, err := jobvitePage(ctx, httpClient, company, page)
			if err != nil {
				yield(nil, err)

				return
			}

			// "ng-non-bindable" marks Jobvite's end-of-results message.
			if jobviteHasAttr(doc, "p", "ng-non-bindable") || jobviteDeprecated(doc) {
				return
			}

			var (
				found   int
				stopped bool
			)

			// walk visits the tree, stopping the moment the consumer stops
			// wanting postings. The stop must propagate out of every level of the
			// recursion: returning only from the current level would keep calling
			// yield after it returned false, which panics.
			var walk func(*html.Node)

			walk = func(n *html.Node) {
				if stopped {
					return
				}

				if posting, ok := jobviteJobLink(n, company); ok {
					found++

					if !yield(posting, nil) {
						stopped = true

						return
					}
				}

				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if stopped {
						return
					}

					walk(c)
				}
			}

			walk(doc)

			if stopped {
				return
			}

			// Counted per page, not cumulatively: a cumulative count never
			// reaches zero once any page has matched, so an empty later page
			// would page forever.
			if found == 0 {
				return
			}
		}
	}
}
