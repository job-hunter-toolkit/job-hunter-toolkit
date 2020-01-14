package jobpostings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type workdayInfo struct {
	Body struct {
		Children []struct {
			Children []struct {
				ListItems []struct {
					Title struct {
						Instances []struct {
							Text string `json:"text"`
						} `json:"instances"`
						CommandLink string `json:"commandLink"`
					} `json:"title"`
					Subtitles []struct {
						Instances []struct {
							Text string `json:"text"`
						} `json:"instances"`
					} `json:"subtitles"`
				} `json:"listItems"`
			} `json:"children"`
		} `json:"children"`
	} `json:"body"`
}

// getWorkdayJobPostings finds JobPostings for companies using https://myworkdayjobs.com
func getWorkdayJobPostings(ctx context.Context, rawURL string) (<-chan *JobPosting, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		fmt.Println("parse:", err)
		return nil, err
	}

	company := strings.Split(parsedURL.Hostname(), ".")[0]

	baseURL := "https://" + parsedURL.Host

	jobPostings := make(chan *JobPosting)

	page := 0

	go func() {
		defer close(jobPostings)

		for {
			req, err := http.NewRequest("GET", rawURL, nil)

			if err != nil {
				break
			}

			if page != 0 {
				req.URL.Path = fmt.Sprintf("%s/fs/searchPagination/318c8bb6f553100021d223d9780d30be/%d", req.URL.Path, page)
			}

			req.Header.Set("Accept", "application/json")

			resp, err := HTTPClient.Do(req)
			if err != nil {
				break
			}

			doc := workdayInfo{}

			err = json.NewDecoder(resp.Body).Decode(&doc)
			if err != nil {
				break
			}
			resp.Body.Close()

			if len(doc.Body.Children[0].Children[0].ListItems) == 0 {
				break
			}

			for _, item := range doc.Body.Children[0].Children[0].ListItems {
				url := baseURL + item.Title.CommandLink
				titleStr := item.Title.Instances[0].Text
				locationStr := item.Subtitles[0].Instances[0].Text

				jobPostings <- &JobPosting{
					Company:  company,
					URL:      url,
					Title:    titleStr,
					Location: locationStr,
				}
			}

			page += 50
		}
	}()

	return jobPostings, nil
}
