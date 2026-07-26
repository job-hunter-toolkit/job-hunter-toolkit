package companies

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"time"

	jobpostings "github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// uberCompany is the identifier this employer is crawled and filtered by.
const uberCompany = "uber"

type uberInfo struct {
	//Status string `json:"status"`
	Data struct {
		Results []uberResult `json:"results"`

		// TotalResults is how many postings the search matched in total, split
		// into low/high words the way this API serialises large integers. Only
		// Low is modelled: the board has never been near 2^32 postings, and the
		// field is used only to stop paginating, so a wrong reading of High
		// could only cause an extra request.
		TotalResults struct {
			Low int `json:"low"`
			//High     int  `json:"high"`
			//Unsigned bool `json:"unsigned"`
		} `json:"totalResults"`
	} `json:"data"`
}

// uberResult is one posting in a page of Uber's job search.
//
// The fields below Title were all present in the response this struct was
// transcribed from and were commented out; the search endpoint has therefore
// been downloading them on every request and the decoder throwing them away.
// They are the richest per-posting payload of any source in this project.
//
// Two groups stay commented out, for two different reasons.
//
// The manager and recruiter blocks are personal data: names, roles, and work
// email addresses of individuals who did not publish a job, they were merely
// named on one. docs/architecture-roadmap.md already forbids sensitive or
// high-cardinality values in metric labels; putting them into postings that get
// written to stdout, to jobs_record.txt, and into anyone's shell history is a
// bigger decision than "the bytes were free", and it is not this change's to
// make.
//
// Description and internalDescription are large and there is nowhere to put them:
// the schema has no description field, and prose extraction is still behind the
// unbuilt per-crawl options flag (docs/compensation.md). They are named here so
// that whoever wires that up knows Uber's body arrives free, in the response
// already fetched.
//
// The dates are string rather than time.Time on purpose. A time.Time field
// fails the whole decode when a board sends a spelling other than RFC 3339, and
// one failed decode is the entire source; a string cannot fail, and
// [uberTimestamp] simply declines to parse what it does not recognise.
type uberResult struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	//Description         string `json:"description"`
	//InternalDescription string `json:"internalDescription"`
	//ManagerID           int    `json:"managerID"`
	Department string `json:"department"`
	Type       string `json:"type"`
	//ProgramAndPlatform  string `json:"programAndPlatform"`
	Location struct {
		Country string `json:"country"`
		Region  string `json:"region"`
		City    string `json:"city"`
	} `json:"location"`
	//Featured           bool        `json:"featured"`
	Level        string `json:"level"`
	CreationDate string `json:"creationDate"`
	//OtherLevels        interface{} `json:"otherLevels"`
	Team string `json:"team"`
	//PortalID           string      `json:"portalID"`
	//IsPipeline         bool        `json:"isPipeline"`
	//ManagerFirstName   string      `json:"managerFirstName"`
	//ManagerLastName    string      `json:"managerLastName"`
	//ManagerEmail       string      `json:"managerEmail"`
	//ManagerRole        string      `json:"managerRole"`
	//RecruiterID        int         `json:"recruiterID"`
	//RecruiterFirstName string      `json:"recruiterFirstName"`
	//RecruiterLastName  string      `json:"recruiterLastName"`
	//RecruiterEmail     string      `json:"recruiterEmail"`
	//StatusID           string      `json:"statusID"`
	//StatusName         string      `json:"statusName"`
	UpdatedDate string `json:"updatedDate"`
	//UniqueSkills       string      `json:"uniqueSkills"`
	TimeType string `json:"timeType"`
}

// uberTimestampLayouts are the spellings accepted for creationDate and
// updatedDate. Only unambiguous ones: a slash-separated date is read one way in
// the United States and the other way almost everywhere else, and the payload
// says nothing about which is meant.
var uberTimestampLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// uberTimestamp converts one of the search's dates to UTC, reporting false when
// it is missing or in a spelling this does not know.
//
// Storing UTC is what makes a comparison between two postings from two
// platforms a comparison of instants rather than of formats.
func uberTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	for _, layout := range uberTimestampLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), true
		}
	}

	return time.Time{}, false
}

// uberEmploymentType normalizes Uber's two engagement fields onto the canonical
// vocabulary.
//
// timeType is the hours ("Full Time", "Part Time") and type is the tenure
// ("Regular", "Intern"), so timeType is asked first and type answers only for
// the postings whose hours are unstated. "Regular" deliberately normalizes to
// nothing: a permanent part-time role is an ordinary thing, so reading tenure as
// hours would invent a fact.
func uberEmploymentType(result uberResult) jobpostings.EmploymentType {
	for _, raw := range []string{result.TimeType, result.Type} {
		if employment, ok := jobpostings.NormalizeEmploymentType(raw); ok {
			return employment
		}
	}

	return jobpostings.EmploymentTypeUnknown
}

// uberPageSize is the number of postings requested per page.
const uberPageSize = 100

// uberMaxPages bounds how many pages the search may be asked for.
//
// This loop used to end only when a page came back empty, so a search backend
// that ignored "page" would be crawled until the crawl deadline. Replayed
// against a stub that answers every page with the same full page, the sibling
// adapters in internal/services issued 5,001 requests and yielded 500,001
// duplicate postings in under a second each; totalResults and the repeated-page
// check below are the real stops, and this is the backstop for a board that
// reports no total and keeps varying its pages. At 100 postings per page it
// allows 50,000 postings, far above what Uber publishes.
const uberMaxPages = 500

// uberSearchURL is the search endpoint the careers site's own front end posts to.
const uberSearchURL = "https://www.uber.com/api/loadSearchJobsResults"

// uberPage fetches a single page of Uber's job search.
//
// Split out so the response body is closed when the page is done. The request
// loop used to carry `defer resp.Body.Close()` inside itself, which holds every
// page's body open until the whole adapter returns; that is not merely untidy,
// internal/httpx hands back its per-host limiter slot only when a body is
// closed, so a paginating adapter that holds bodies open eventually deadlocks
// against the limit and waits out the client timeout.
func uberPage(ctx context.Context, httpClient *http.Client, page int) (*uberInfo, error) {
	body := strings.NewReader(fmt.Sprintf(`{"limit":%d,"page":%d,"params":{}}`, uberPageSize, page))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uberSearchURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Uber page %d: %w", page, err)
	}

	req.Header.Set("Content-Type", "application/json")

	// what is love, baby don't hurt me, don't hurt me, no more
	req.Header.Set("X-Csrf-Token", "<3")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Uber for page %d: %w", page, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code from Uber for page %d: %s", page, resp.Status)
	}

	doc := new(uberInfo)

	if err := json.NewDecoder(resp.Body).Decode(doc); err != nil {
		return nil, fmt.Errorf("failed to decode response from Uber for page %d: %w", page, err)
	}

	return doc, nil
}

// uberResultsFingerprint identifies a page by the postings on it, so a search
// backend that ignores "page" and serves the same results forever can be
// recognised and the loop ended.
//
// A fingerprint per page rather than a set of every posting id: the set grows
// with the board, the fingerprint set grows with the number of pages.
func uberResultsFingerprint(doc *uberInfo) uint64 {
	sum := fnv.New64a()

	for _, item := range doc.Data.Results {
		// The separator keeps {1, 23} from fingerprinting the same as {12, 3}.
		_, _ = sum.Write([]byte(strconv.Itoa(item.ID)))
		_, _ = sum.Write([]byte{0})
	}

	return sum.Sum64()
}

// Uber finds JobPostings found at https://www.uber.com/api/loadSearchJobsResults
//
// It returns [jobpostings.Jobs] rather than a bare sequence so that a broken
// board is reported as a broken board. The previous signature carried no error
// channel at all and swallowed every page failure, which made an unreachable
// endpoint indistinguishable from an employer with nothing open — the
// silently-empty source this project treats as its worst failure mode. Matching
// the shape every other adapter uses is also what lets this be registered in the
// crawl; see [Sources].
func Uber(ctx context.Context, httpClient *http.Client) jobpostings.Jobs {
	return func(yield func(*jobpostings.JobPosting, error) bool) {
		var (
			seenPages = make(map[uint64]struct{})
			fetched   int
		)

		for page := range uberMaxPages {
			doc, err := uberPage(ctx, httpClient, page)
			if err != nil {
				yield(nil, err)

				return
			}

			if len(doc.Data.Results) == 0 {
				return
			}

			fingerprint := uberResultsFingerprint(doc)

			// Checked before anything is yielded, so a backend that ignores
			// "page" costs one wasted request rather than an endless stream of
			// duplicates.
			if _, repeated := seenPages[fingerprint]; repeated {
				return
			}

			seenPages[fingerprint] = struct{}{}

			for _, item := range doc.Data.Results {
				if ctx.Err() != nil {
					yield(nil, ctx.Err())

					return
				}

				var (
					url         = "https://www.uber.com/global/en/careers/list/" + fmt.Sprintf("%d", item.ID)
					titleStr    = strings.TrimSpace(item.Title)
					locationStr = strings.TrimSpace(fmt.Sprintf("%s, %s, %s", item.Location.Country, item.Location.Region, item.Location.City))
				)

				posting := &jobpostings.JobPosting{
					Company:  uberCompany,
					URL:      url,
					Title:    titleStr,
					Location: locationStr,

					Department:     strings.TrimSpace(item.Department),
					Team:           strings.TrimSpace(item.Team),
					EmploymentType: uberEmploymentType(item),
					// "level" is Uber's own ladder rung ("5a", "Senior"), which
					// is why Seniority is a free string: any canonical mapping
					// would be this project's opinion about another company's job
					// architecture.
					Seniority:  strings.TrimSpace(item.Level),
					ExternalID: strconv.Itoa(item.ID),
					Source:     jobpostings.PostingSource{Platform: DirectPlatform, Key: uberCompany},
				}

				if created, ok := uberTimestamp(item.CreationDate); ok {
					posting.PostedAt = created
				}

				if updated, ok := uberTimestamp(item.UpdatedDate); ok {
					posting.UpdatedAt = updated
				}

				if !yield(posting, nil) {
					// If the yield function returns false, stop processing further job postings
					return
				}
			}

			fetched += len(doc.Data.Results)

			// The search reports how many postings it matched, so use it rather
			// than probing for an empty page.
			if total := doc.Data.TotalResults.Low; total > 0 && fetched >= total {
				return
			}

			if len(doc.Data.Results) < uberPageSize {
				return
			}
		}
	}
}
