package jabba

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type greenHouseJobs struct {
	Jobs []struct {
		AbsoluteURL string `json:"absolute_url"`
		//InternalJobID int    `json:"internal_job_id"`
		Location struct {
			Name string `json:"name"`
		} `json:"location"`
		//Metadata      interface{}   `json:"metadata"`
		//ID            int           `json:"id"`
		//UpdatedAt     string        `json:"updated_at"`
		//RequisitionID interface{}   `json:"requisition_id"`
		Title string `json:"title"`
		//Content       string        `json:"content"`
		//Departments   []interface{} `json:"departments"`
		//Offices       []struct {
		//	ID       int           `json:"id"`
		//	Name     string        `json:"name"`
		//	Location string        `json:"location"`
		//	ChildIds []interface{} `json:"child_ids"`
		//	ParentID interface{}   `json:"parent_id"`
		//} `json:"offices"`
	} `json:"jobs"`
	//Meta struct {
	//	Total int `json:"total"`
	//} `json:"meta"`
}

// curl 'https://boards-api.greenhouse.io/v1/boards/company/jobs?content=true' | jq .
func getGreenHouseJobsFor(ctx context.Context, company string) (<-chan *JobPosting, error) {
	// To include job description, simply add
	// ?content=true as a URL param to the request
	req, err := http.NewRequest("GET", "https://boards-api.greenhouse.io/v1/boards/"+company+"/jobs", nil)
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

	doc := greenHouseJobs{}

	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return nil, err
	}

	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		for _, item := range doc.Jobs {
			url := strings.TrimSpace(strings.Replace(item.AbsoluteURL, "http://", "https://", -1))
			titleStr := strings.TrimSpace(item.Title)
			locationStr := strings.TrimSpace(item.Location.Name)

			jobPostings <- &JobPosting{
				URL:      url,
				Title:    titleStr,
				Location: locationStr,
			}
		}
	}()

	return jobPostings, nil
}
