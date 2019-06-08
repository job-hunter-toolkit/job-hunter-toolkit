package jobpostings

import (
	"fmt"
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type uberInfo struct {
	//Status string `json:"status"`
	Data   struct {
		Results []struct {
			ID                  int    `json:"id"`
			Title               string `json:"title"`
			//Description         string `json:"description"`
			//InternalDescription string `json:"internalDescription"`
			//ManagerID           int    `json:"managerID"`
			//Department          string `json:"department"`
			//Type                string `json:"type"`
			//ProgramAndPlatform  string `json:"programAndPlatform"`
			Location            struct {
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

// GetUberJobPostings finds JobPostings found at https://www.uber.com/api/loadSearchJobsResults
func GetUberJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	body := strings.NewReader("{\"limit\":5000,\"page\":0,\"params\":{}}")
	req, err := http.NewRequest("POST", "https://www.uber.com/api/loadSearchJobsResults", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Csrf-Token", "hire me: https://picatz.github.io/") // lol
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc := uberInfo{}

	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return nil, err
	}

	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		for _, item := range doc.Data.Results {
			url := "https://www.uber.com/global/en/careers/list/" + fmt.Sprintf("%d", item.ID)
			titleStr := strings.TrimSpace(item.Title)
			locationStr := strings.TrimSpace(fmt.Sprintf("%s, %s, %s", item.Location.Country, item.Location.Region, item.Location.City))

			jobPostings <- &JobPosting{
				URL:      url,
				Title:    titleStr,
				Location: locationStr,
			}
		}
	}()

	return jobPostings, nil
}
