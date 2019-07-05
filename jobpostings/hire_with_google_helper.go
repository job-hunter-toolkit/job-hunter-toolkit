package jobpostings

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type hireWithGoogleJobs []struct {
	//Context            string    `json:"@context"`
	//Type               string    `json:"@type"`
	//DatePosted         time.Time `json:"datePosted"`
	//Description        string    `json:"description"`
	//HiringOrganization struct {
	//	Type       string `json:"@type"`
	//	Name       string `json:"name"`
	//	Department struct {
	//		Type string `json:"@type"`
	//		Name string `json:"name"`
	//	} `json:"department"`
	//	Logo string `json:"logo"`
	//} `json:"hiringOrganization"`
	//Identifier struct {
	//	Type  string `json:"@type"`
	//	Name  string `json:"name"`
	//	Value string `json:"value"`
	//} `json:"identifier"`
	JobLocation struct {
		Type    string `json:"@type"`
		Address struct {
			Type            string `json:"@type"`
			AddressLocality string `json:"addressLocality"`
			//AddressRegion   string `json:"addressRegion"`
			AddressCountry string `json:"addressCountry"`
		} `json:"address"`
	} `json:"jobLocation"`
	Title string `json:"title"`
	URL   string `json:"url"`
	//EmploymentType string `json:"employmentType"`
}

// curl 'https://hire.withgoogle.com/v2/api/t/company/public/jobs/' | jq .
func getHireWithGoogleJobPostingsFor(ctx context.Context, company string) (<-chan *JobPosting, error) {
	req, err := http.NewRequest("GET", "https://hire.withgoogle.com/v2/api/t/"+company+"/public/jobs/", nil)
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

	doc := hireWithGoogleJobs{}

	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return nil, err
	}

	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		for _, item := range doc {
			url := strings.TrimSpace(strings.Replace(item.URL, "http://", "https://", -1))
			titleStr := strings.TrimSpace(item.Title)
			locationStr := "unknown"
			if item.JobLocation.Type == "@Place" || item.JobLocation.Type == "Place" {
				locationStr = strings.Join([]string{item.JobLocation.Address.AddressLocality, item.JobLocation.Address.AddressCountry}, ",")
			}
			jobPostings <- &JobPosting{
				Company:  company,
				URL:      url,
				Title:    titleStr,
				Location: locationStr,
			}
		}
	}()

	return jobPostings, nil
}
