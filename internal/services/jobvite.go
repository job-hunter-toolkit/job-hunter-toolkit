package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

// jobvitePlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const jobvitePlatform = "jobvite"

func init() {
	registerBuiltin(jobvitePlatform, multiJobsFunc(Jobvite, JobviteCompanies))
}

// jobviteMaxPages bounds how many pages a single Jobvite tenant may be asked
// for.
//
// Jobvite publishes no total, and the search template answers an out-of-range
// page with whatever it feels like, so before this the loop ended only when a
// page happened to contain no job links. A tenant that ignores "p" paginates
// until the crawl deadline: the same shape, measured on the sibling adapters,
// produced 5,001 requests and 500,001 duplicate postings against a stub in under
// a second. [pageRepeatGuard] handles the repeated-page case; this is the
// backstop. Jobvite serves a few dozen postings per page, so 500 pages is far
// more than any tenant in [JobviteCompanies] publishes.
const jobviteMaxPages = 500

var JobviteCompanies = []string{
	"actionet",
	"affcareers",
	"anthology",
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
	"northwest-center",
	"nutanix",
	"paloaltonetworks",
	"pilgrims",
	"pointofrental",
	"reveal",
	"securityfinance",
	"splunk-careers",
	"torrancememorialjobs",
	"tylertech",
	"versa-networks",
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

	posting := &internal.JobPosting{
		Company:  company,
		URL:      "https://jobs.jobvite.com" + href,
		Title:    title,
		Location: "unknown",

		ExternalID: jobviteExternalID(href),
		Source:     internal.PostingSource{Platform: jobvitePlatform, Key: company},
	}

	cells := jobviteRowCells(n)

	if len(cells) > jobviteLocationCell && cells[jobviteLocationCell] != "" {
		posting.Location = cells[jobviteLocationCell]
	}

	// The cells between the title and the location were previously stepped over
	// three at a time and never read, even though they were already parsed and
	// in memory. They carry no headers, and which of them holds what varies by
	// tenant template, so each is identified by its contents rather than by its
	// position: a cell that is recognisably an employment type on its own, or
	// that parses as an unambiguous date, is taken as one.
	//
	// Nothing is read as a department. That would have to be "whatever is left
	// over", and a leftover cell is as likely to hold a requisition number or a
	// brand as an org unit — a wrong department is invisible to the person
	// filtering on it, while an empty one is not.
	for index, cell := range cells {
		if index == 0 || index == jobviteLocationCell || cell == "" {
			continue
		}

		if posting.EmploymentType == internal.EmploymentTypeUnknown {
			if employment, ok := employmentTypeFromUnlabelled(cell); ok {
				posting.EmploymentType = employment

				continue
			}
		}

		if posting.PostedAt.IsZero() {
			if posted, ok := jobvitePostedAt(cell); ok {
				posting.PostedAt = posted
			}
		}
	}

	return posting, true
}

// jobviteLocationCell is the index, within a search row, of the cell holding the
// posting's location.
const jobviteLocationCell = 3

// jobviteExternalID returns Jobvite's own id for a posting, taken from the
// "/job/<id>" segment of its link, or "" if the link is not shaped that way.
func jobviteExternalID(href string) string {
	_, id, found := strings.Cut(href, "/job/")
	if !found {
		return ""
	}

	// A tenant that appends tracking parameters must not turn them into part of
	// the identifier.
	id, _, _ = strings.Cut(id, "?")
	id, _, _ = strings.Cut(id, "#")

	return strings.Trim(id, "/")
}

// jobviteDateLayouts are the date spellings accepted from a search row.
//
// Slash-separated dates are deliberately absent: 03/04 is the third of April to
// half the world and the fourth of March to the other half, and a Jobvite row
// carries nothing that says which a tenant means. A date a month wrong would
// reach [internal.Filter.PostedSince] with nothing downstream able to notice.
var jobviteDateLayouts = []string{
	"Jan 2, 2006",
	"January 2, 2006",
	"2 Jan 2006",
	"2006-01-02",
}

// jobvitePostedAt reads a row cell as a posting date, reporting false when it is
// not one.
func jobvitePostedAt(cell string) (time.Time, bool) {
	for _, layout := range jobviteDateLayouts {
		if posted, err := time.Parse(layout, cell); err == nil {
			return posted.UTC(), true
		}
	}

	return time.Time{}, false
}

// jobviteRowCells returns the whitespace-normalised text of every cell in the
// table row holding a job link, or nil if the page does not have the expected
// shape.
//
// Every hop is checked because the path is fragile: Jobvite serves several page
// templates, and an unexpected one must not panic a crawl of a thousand
// companies.
func jobviteRowCells(n *html.Node) []string {
	if n.Parent == nil || n.Parent.Parent == nil {
		return nil
	}

	var cells []string

	for cell := n.Parent.Parent.FirstChild; cell != nil; cell = cell.NextSibling {
		cells = append(cells, strings.Join(strings.Fields(getText(cell)), " "))
	}

	return cells
}

// Jobvite returns the job postings for a company hosted on Jobvite.
//
// Note that Jobvite has been migrating tenants to a client-side rendered
// template that serves no job links in its HTML. Those boards parse as empty
// rather than failing; see docs/source-backlog.md.
func Jobvite(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		var pages pageRepeatGuard

		// Jobvite's search is zero-indexed. Starting at one silently skipped the
		// first (and often only) page, making active tenants appear empty.
		for page := range jobviteMaxPages {
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

			// The page's postings are collected before any of them is yielded so
			// a page that merely repeats one already served can be recognised
			// and dropped whole. Collecting also removes the reason the walk
			// used to carry a "stopped" flag through every level of the
			// recursion: yield is no longer called from inside it, so there is
			// nothing to stop calling.
			var (
				postings []*internal.JobPosting
				walk     func(*html.Node)
			)

			walk = func(n *html.Node) {
				if posting, ok := jobviteJobLink(n, company); ok {
					postings = append(postings, posting)
				}

				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}

			walk(doc)

			// Counted per page, not cumulatively: a cumulative count never
			// reaches zero once any page has matched, so an empty later page
			// would page forever.
			if len(postings) == 0 {
				return
			}

			ids := make([]string, 0, len(postings))
			for _, posting := range postings {
				ids = append(ids, posting.URL)
			}

			if pages.repeated(ids) {
				return
			}

			for _, posting := range postings {
				if !yield(posting, nil) {
					return
				}
			}
		}

		yield(nil, fmt.Errorf("refusing to keep paginating Jobvite for company %q: the search was still serving job links after %d pages", company, jobviteMaxPages))
	}
}
