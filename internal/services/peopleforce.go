package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

// peopleForcePlatform is the ATS family this file registers, and the value that
// reaches [internal.PostingSource.Platform].
const peopleForcePlatform = "peopleforce"

func init() {
	registerBuiltin(peopleForcePlatform, multiJobsFunc(PeopleForce, PeopleForceCompanies))
}

// peopleForceMaxPages bounds how many careers pages a single PeopleForce tenant
// may be asked for.
//
// Pagination no longer stops merely because the "Displaying X - Y of Z" marker
// is missing (see [PeopleForce]), so the loop needs its own bound: a board that
// answers every "?page=N" with page one would otherwise run to the crawl
// deadline, the way the sibling adapters did, 5,001 requests and 500,001
// duplicate postings against a stub in under a second, before they were bounded.
// [pageRepeatGuard] catches exactly that case; this is the backstop. PeopleForce
// tenants are small, a few pages each, so 200 is far beyond any of them.
const peopleForceMaxPages = 200

// errPeopleForceNoJobList marks a page that carried no job list at all, either
// because the server answered 404/410 or because the "results" container was
// absent.
//
// On the first page that is a broken source and must be reported. Past the first
// page it is how a board without a "Displaying X - Y of Z" marker says "there is
// no such page", so it ends pagination quietly instead of failing a source that
// was crawled successfully.
var errPeopleForceNoJobList = errors.New("no job list on page")

var PeopleForceCompanies = []string{
	"altegio",
	"appolica",
	"azendo",
	"balticpower",
	"bcdtriptech",
	"cashea",
	"debridge",
	"docusketch",
	"epc",
	"eratosthenes",
	"hacken",
	"iihl",
	"kagi",
	"kyivindependent",
	"laam",
	"leadsdoit",
	"litebox",
	"ltvplus",
	"maudau",
	"nagateam",
	"neuroleadership",
	"nove8",
	"openit",
	"planatechnologies",
	"prosteergroup",
	"saltedge",
	"skyhighgrowth",
	"softcom",
	"spacedev",
	"speedandfunction",
	"swarmer",
	"sweatworks",
	"team", // for peopleforce.io itself
	"truora",
	"unitedsoftware",
	"vyriy",
	"youscan",
}

// scrapeJobs retrieves and processes the given pageURL, returning the found job postings
// along with the parsed HTML document (for pagination inspection).
func scrapeJobs(ctx context.Context, httpClient *http.Client, pageURL string) ([]*internal.JobPosting, *html.Node, error) {
	// Parse the base URL to resolve relative links.
	baseURL, err := url.Parse(pageURL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid URL %q: %w", pageURL, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request failed: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching URL failed: %w", err)
	}
	defer resp.Body.Close()

	// A page number past the end of a board is answered with a 404 by some
	// tenants and with a job-less page by others; both mean the same thing, so
	// both carry the same sentinel.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, nil, fmt.Errorf("%w: %q responded %d", errPeopleForceNoJobList, pageURL, resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("unexpected response status: %d", resp.StatusCode)
	}

	// Parse the HTML document.
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing HTML failed: %w", err)
	}

	// Limit our search to the container with id="results".
	resultsNode := findElementByID(doc, "results")
	if resultsNode == nil {
		return nil, doc, fmt.Errorf("%w: %q has no results container", errPeopleForceNoJobList, pageURL)
	}

	var postings []*internal.JobPosting
	// Find all anchor elements with hrefs starting with "/careers/v/".
	jobLinks := findJobLinks(resultsNode)
	for _, a := range jobLinks {
		var jp internal.JobPosting
		// Process the href attribute to resolve the absolute URL.
		for _, attr := range a.Attr {
			if attr.Key == "href" && strings.HasPrefix(attr.Val, "/careers/v/") {
				relative, err := url.Parse(attr.Val)
				if err != nil {
					return nil, doc, fmt.Errorf("parsing relative URL failed: %w", err)
				}
				jp.URL = baseURL.ResolveReference(relative).String()
				// The path segment after /careers/v/ is the board's own id for
				// the posting, which outlives a URL the tenant may restyle.
				jp.ExternalID = strings.Trim(strings.TrimPrefix(relative.Path, "/careers/v/"), "/")
				break
			}
		}
		// Use the anchor text for the job title.
		jp.Title = strings.TrimSpace(getText(a))
		// Optionally, extract details (e.g. "Engineering, Full Time Position, Any - Remote")
		// and derive the location by taking the last comma-delimited segment.
		if detailsNode := findSiblingDiv(a.Parent); detailsNode != nil {
			details := strings.TrimSpace(getText(detailsNode))
			if details != "" {
				peopleForceDetails(&jp, details)
			}
		}
		postings = append(postings, &jp)
	}

	return postings, doc, nil
}

// peopleForceDetails reads a posting's details line into the posting.
//
// The board renders that line as "<department>, <employment type>, <location>" —
// the comment above the call site has documented that shape since this adapter
// was written, and the implementation then kept only the last segment and threw
// the other two away, after having already parsed them into a string in memory.
//
// The department is only taken when there are three or more segments. Two are
// ambiguous: "Kyiv, Ukraine" is a location that happens to contain a comma, and
// reading its city as a department would file real postings under a department
// that does not exist. An employment type is safe to look for in either shape,
// because a place name does not normalise to one.
//
// The location segment is also offered to [internal.NormalizeWorkplaceType],
// which is the one place this project does that deliberately. PeopleForce puts
// its structured workplace choice in this slot and renders it as text: "Any -
// Remote", "Hybrid", "Office". A real place name simply fails to normalise and
// leaves the field empty, and the "Remote, OR" trap that makes location text
// untrustworthy elsewhere cannot bite here, because splitting on commas puts the
// Oregon abbreviation in its own segment.
func peopleForceDetails(posting *internal.JobPosting, details string) {
	parts := strings.Split(details, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	posting.Location = parts[len(parts)-1]

	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			continue
		}

		if employment, ok := internal.NormalizeEmploymentType(part); ok && posting.EmploymentType == internal.EmploymentTypeUnknown {
			posting.EmploymentType = employment

			continue
		}

		if len(parts) > 2 && posting.Department == "" {
			posting.Department = part
		}
	}

	if workplace, ok := internal.NormalizeWorkplaceType(posting.Location); ok {
		posting.WorkplaceType = workplace
	}
}

// PeopleForce returns a jobpostings.Jobs function that iterates over paginated PeopleForce
// career pages for the given company.
//
// Pagination used to depend entirely on the "Displaying X - Y of Z in total"
// string that [extractTotalCount] scrapes: a board that did not render it, and
// nothing guarantees one does, small boards omit it, a localised or restyled
// template hides it, was silently cut off after page one. That reported a
// plausible non-zero count rather than an error, so `health` marked the source
// ok and the truncation was invisible; it is the same shape as the
// filter-applied-twice incident this project already has a scar from.
//
// The total is now an early exit only, never the sole permission to continue.
// Without it the adapter keeps asking for the next page until the board runs out
// of them: an empty page, a 404, a page with no results container (see
// [errPeopleForceNoJobList]), or a page that merely repeats one already served
// (see [pageRepeatGuard]).
func PeopleForce(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		// Construct the base URL for the company’s careers page.
		baseURL := fmt.Sprintf("https://%s.peopleforce.io/careers", company)

		var (
			pages        pageRepeatGuard
			runningCount int
			totalCount   int
			totalFound   bool
		)

		for page := 1; page <= peopleForceMaxPages; page++ {
			// Construct the current page URL.
			pageURL := baseURL
			if page > 1 {
				pageURL = fmt.Sprintf("%s?page=%d", baseURL, page)
			}

			postings, doc, err := scrapeJobs(ctx, httpClient, pageURL)
			if err != nil {
				// Past the first page, no job list means there is no such page,
				// which is the end of a board that publishes no total; on the
				// first page it means the source is broken and must be reported.
				if page > 1 && errors.Is(err, errPeopleForceNoJobList) {
					return
				}

				yield(nil, fmt.Errorf("failed to scrape PeopleForce careers page %q for company %q: %w", pageURL, company, err))

				return
			}

			// On the first page, try to extract the total job count.
			if !totalFound {
				if total, ok := extractTotalCount(doc); ok {
					totalCount = total
					totalFound = true
				}
			}

			// If no postings are returned, we stop paginating.
			if len(postings) == 0 {
				return
			}

			ids := make([]string, 0, len(postings))
			for _, job := range postings {
				ids = append(ids, job.URL)
			}

			// Checked before anything is yielded: a tenant that answers an
			// out-of-range "?page=" with page one is common, and without the
			// total to stop us it would otherwise be crawled forever.
			if pages.repeated(ids) {
				return
			}

			// Yield each job posting (setting the company field).
			for _, job := range postings {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				job.Company = company
				job.Source = internal.PostingSource{Platform: peopleForcePlatform, Key: company}

				if !yield(job, nil) {
					return
				}
				runningCount++
			}

			// If we've reached or exceeded the reported total, we're done.
			if totalFound && runningCount >= totalCount {
				return
			}
		}

		yield(nil, fmt.Errorf("refusing to keep paginating PeopleForce for company %q: the careers board was still serving postings after %d pages", company, peopleForceMaxPages))
	}
}

// extractTotalCount scans the document for an element (like a <p> or <div>)
// whose text is of the form "Displaying X - Y of Z in total" and returns Z.
func extractTotalCount(doc *html.Node) (int, bool) {
	// The regex matches "Displaying" followed by two numbers (X and Y), a dash between them,
	// then "of" and finally the total count (Z) in a capture group.
	re := regexp.MustCompile(`Displaying\s+\d+\s*-\s*\d+\s+of\s+(\d+)`)
	var total int
	var found bool
	var search func(*html.Node)
	search = func(n *html.Node) {
		if found {
			return
		}
		// We only check certain element types.
		if n.Type == html.ElementNode && (n.Data == "p" || n.Data == "div") {
			text := getText(n)
			// trim to remove extra whitespace
			text = strings.TrimSpace(text)
			matches := re.FindStringSubmatch(text)
			if len(matches) == 2 {
				var err error
				total, err = strconv.Atoi(matches[1])
				if err == nil {
					found = true
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if !found {
				search(c)
			}
		}
	}
	search(doc)
	return total, found
}

// findElementByID recursively searches for an element with the given id.
func findElementByID(n *html.Node, id string) *html.Node {
	var found *html.Node
	var search func(*html.Node)
	search = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				if attr.Key == "id" && attr.Val == id {
					found = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			search(c)
		}
	}
	search(n)
	return found
}

// findJobLinks searches within node n for anchor (<a>) elements with hrefs starting with "/careers/v/".
func findJobLinks(n *html.Node) []*html.Node {
	var links []*html.Node
	var search func(*html.Node)
	search = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" && strings.HasPrefix(attr.Val, "/careers/v/") {
					links = append(links, n)
					break
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

// findSiblingDiv attempts to find a sibling <div> element relative to node n.
// This is used to extract details text (e.g. location info).
func findSiblingDiv(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	parent := n.Parent
	if parent == nil {
		return nil
	}
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c != n && c.Type == html.ElementNode && c.Data == "div" {
			return c
		}
	}
	return nil
}

// getText recursively extracts the text content from an HTML node.
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
