package jobpostings

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type leverJobs []struct {
	//AdditionalPlain string `json:"additionalPlain"`
	//Additional      string `json:"additional"`
	Categories      struct {
		//Commitment string `json:"commitment"`
		//Department string `json:"department"`
		//Level      string `json:"level"`
		Location   string `json:"location"`
		//Team       string `json:"team"`
	} `json:"categories,omitempty"`
	//CreatedAt        int64  `json:"createdAt"`
	//DescriptionPlain string `json:"descriptionPlain"`
	//Description      string `json:"description"`
	//ID               string `json:"id"`
	//Lists            []struct {
	//	Text    string `json:"text"`
	//	Content string `json:"content"`
	//} `json:"lists"`
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
	//ApplyURL   string `json:"applyUrl"`
}

// curl 'https://api.lever.co/v0/postings/company?&mode=json' | jq .
func getLeverJobsFor(ctx context.Context, company string) (<-chan *JobPosting, error) {
	req, err := http.NewRequest("GET", "https://api.lever.co/v0/postings/"+company, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc := leverJobs{}

	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return nil, err
	}

	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		for _, item := range doc {
			url := strings.TrimSpace(strings.Replace(item.HostedURL, "http://", "https://", -1))
			titleStr := strings.TrimSpace(item.Text)
			locationStr := strings.TrimSpace(item.Categories.Location)

			jobPostings <- &JobPosting{
				URL:      url,
				Title:    titleStr,
				Location: locationStr,
			}
		}
	}()

	return jobPostings, nil
}
