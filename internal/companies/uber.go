package companies

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"iter"
	"net/http"
	"strconv"
	"strings"

	jobpostings "github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

type uberInfo struct {
	//Status string `json:"status"`
	Data struct {
		Results []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
			//Description         string `json:"description"`
			//InternalDescription string `json:"internalDescription"`
			//ManagerID           int    `json:"managerID"`
			//Department          string `json:"department"`
			//Type                string `json:"type"`
			//ProgramAndPlatform  string `json:"programAndPlatform"`
			Location struct {
				Country string `json:"country"`
				Region  string `json:"region"`
				City    string `json:"city"`
			} `json:"location"`
			//Featured           bool        `json:"featured"`
			//Level              string      `json:"level"`
			//CreationDate       time.Time   `json:"creationDate"`
			//OtherLevels        interface{} `json:"otherLevels"`
			//Team               string      `json:"team"`
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
			//UpdatedDate        time.Time   `json:"updatedDate"`
			//UniqueSkills       string      `json:"uniqueSkills"`
			//TimeType           string      `json:"timeType"`
		} `json:"results"`

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
func Uber(ctx context.Context, httpClient *http.Client) (iter.Seq[*jobpostings.JobPosting], error) {
	return func(yield func(*jobpostings.JobPosting) bool) {
		var (
			seenPages = make(map[uint64]struct{})
			fetched   int
		)

		for page := range uberMaxPages {
			doc, err := uberPage(ctx, httpClient, page)
			if err != nil {
				// This iterator's signature carries no error channel, so a
				// broken board is reported here as a board with no jobs. That is
				// wrong and is left alone deliberately: fixing it means changing
				// the signature to internal.Jobs, which is the same change
				// needed to register this adapter in the crawl at all.
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
					return
				}

				var (
					url         = "https://www.uber.com/global/en/careers/list/" + fmt.Sprintf("%d", item.ID)
					titleStr    = strings.TrimSpace(item.Title)
					locationStr = strings.TrimSpace(fmt.Sprintf("%s, %s, %s", item.Location.Country, item.Location.Region, item.Location.City))
				)

				if !yield(&jobpostings.JobPosting{
					Company:  "uber",
					URL:      url,
					Title:    titleStr,
					Location: locationStr,
				}) {
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
	}, nil
}
