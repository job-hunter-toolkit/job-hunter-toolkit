package jobpostings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type smartRecruitersJobs struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	TotalFound int `json:"totalFound"`
	Content    []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Location struct {
			City    string `json:"city"`
			Region  string `json:"region"`
			Country string `json:"country"`
		} `json:"location"`
	} `json:"content"`
}

//  curl 'https://api.smartrecruiters.com/v1/companies/company/postings' | jq .
func getSmartRecruitersJobsPostingsFor(ctx context.Context, company string) (<-chan *JobPosting, error) {
	req, err := http.NewRequest("GET", "https://api.smartrecruiters.com/v1/companies/"+company+"/postings", nil)
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

	doc := smartRecruitersJobs{}

	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return nil, err
	}

	nextBatch := func(doc *smartRecruitersJobs, offset int) error {
		req, err := http.NewRequest("GET", fmt.Sprintf("https://api.smartrecruiters.com/v1/companies/%s/postings?offset=%d", company, offset), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")

		req = req.WithContext(ctx)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		err = json.NewDecoder(resp.Body).Decode(doc)

		if err != nil {
			return err
		}

		return nil
	}

	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		for _, item := range doc.Content {
			url := fmt.Sprintf("https://jobs.smartrecruiters.com/%s/%s", company, item.ID)
			titleStr := strings.TrimSpace(item.Name)
			locationStr := strings.Join([]string{item.Location.City, item.Location.Region, item.Location.Country}, ",")

			jobPostings <- &JobPosting{
				URL:      url,
				Title:    titleStr,
				Location: locationStr,
			}
		}

		for i := len(doc.Content); i < doc.TotalFound; i += len(doc.Content) {

			nextBatch(&doc, i)

			for _, item := range doc.Content {
				url := fmt.Sprintf("https://jobs.smartrecruiters.com/%s/%s", company, item.ID)
				titleStr := strings.TrimSpace(item.Name)
				locationStr := strings.Join([]string{item.Location.City, item.Location.Region, item.Location.Country}, ",")

				jobPostings <- &JobPosting{
					URL:      url,
					Title:    titleStr,
					Location: locationStr,
				}
			}
		}
	}()

	return jobPostings, nil
}
