package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"golang.org/x/net/html"
)

func init() {
	registerBuiltin(multiJobsFunc(PeopleForce, PeopleForceCompanies))
}

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
		return nil, doc, fmt.Errorf("results container not found")
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
				parts := strings.Split(details, ",")
				// Use the last part as location (after trimming whitespace).
				jp.Location = strings.TrimSpace(parts[len(parts)-1])
			}
		}
		postings = append(postings, &jp)
	}

	return postings, doc, nil
}

// PeopleForce returns a jobpostings.Jobs function that iterates over paginated PeopleForce
// career pages for the given company. It stops when the running total equals or exceeds
// the total number displayed on the page.
func PeopleForce(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		// Construct the base URL for the company’s careers page.
		baseURL := fmt.Sprintf("https://%s.peopleforce.io/careers", company)
		page := 1
		runningCount := 0
		var totalCount int
		totalFound := false

		for {
			// Construct the current page URL.
			var pageURL string
			if page == 1 {
				pageURL = baseURL
			} else {
				pageURL = fmt.Sprintf("%s?page=%d", baseURL, page)
			}

			postings, doc, err := scrapeJobs(ctx, httpClient, pageURL)
			if err != nil {
				yield(nil, err)
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
				break
			}

			// Yield each job posting (setting the company field).
			for _, job := range postings {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())
					return
				}

				job.Company = company
				if !yield(job, nil) {
					return
				}
				runningCount++
			}

			// If we've reached or exceeded the reported total, we're done.
			if !totalFound || runningCount >= totalCount {
				break
			}

			page++
		}
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
