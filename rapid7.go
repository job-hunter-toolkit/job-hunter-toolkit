package jabba

import (
	"context"
	"net/http"
	"encoding/json"
)

type rapid7Info []struct {
	Title       string `json:"title"`
	Location    string `json:"location"`
	Applyurl    string `json:"applyurl"`
}

// GetRapid7JobPostings finds JobPostings found at https://www.rapid7.com/api/careers/jobsb
func GetRapid7JobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	req, err := http.NewRequest("GET", "https://www.rapid7.com/api/careers/jobs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc := rapid7Info{}

	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return nil, err
	}

	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		for _, item := range doc {
			url := item.Applyurl
			titleStr := item.Title
			locationStr := item.Location

			jobPostings <- &JobPosting{
				URL:      url,
				Title:    titleStr,
				Location: locationStr,
			}
		}
	}()

	return jobPostings, nil
}