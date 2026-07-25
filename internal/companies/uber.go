package companies

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
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
		//TotalResults struct {
		//	Low      int  `json:"low"`
		//	High     int  `json:"high"`
		//	Unsigned bool `json:"unsigned"`
		//} `json:"totalResults"`
	} `json:"data"`
}

// Uber finds JobPostings found at https://www.uber.com/api/loadSearchJobsResults
func Uber(ctx context.Context, httpClient *http.Client) (iter.Seq[*jobpostings.JobPosting], error) {
	bodyTemplate := "{\"limit\":100,\"page\":%d,\"params\":{}}"

	return func(yield func(*jobpostings.JobPosting) bool) {
		page := 0

		for {
			body := strings.NewReader(fmt.Sprintf(bodyTemplate, page))

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.uber.com/api/loadSearchJobsResults", body)
			if err != nil {
				return
			}

			req.Header.Set("Content-Type", "application/json")

			// what is love, baby don't hurt me, don't hurt me, no more
			req.Header.Set("X-Csrf-Token", "<3")

			resp, err := httpClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			doc := uberInfo{}
			err = json.NewDecoder(resp.Body).Decode(&doc)
			if err != nil {
				return
			}

			if len(doc.Data.Results) == 0 {
				break
			}

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

			page++
		}
	}, nil
}
