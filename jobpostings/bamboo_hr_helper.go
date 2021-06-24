package jobpostings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

type bambooInfo map[string]struct {
	ID             string
	JobOpeningName string
	Location       struct {
		City  string
		State string
	}
}

func getBambooHRJobsFor(ctx context.Context, company string) (<-chan *JobPosting, error) {
	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		var findJobs func(string, *html.Node)

		findJobs = func(url string, n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "script" {
				for _, a := range n.Attr {
					if a.Key == "id" && a.Val == "positionData" {
						if n.FirstChild != nil {
							doc := make(bambooInfo)
							err := json.Unmarshal([]byte(n.FirstChild.Data), &doc)
							if err != nil {
								return
							}
							for key, valueStruct := range doc {
								titleStr := strings.TrimSpace(valueStruct.JobOpeningName)
								locationStr := ""
								if valueStruct.Location.City == valueStruct.Location.State {
									locationStr = valueStruct.Location.City
								} else {
									locationStr = strings.Join([]string{valueStruct.Location.City, valueStruct.Location.State}, ", ")
								}
								jobURL := fmt.Sprintf("%sview.php?id=%s", url, key)
								jobPostings <- &JobPosting{
									Company:  company,
									URL:      jobURL,
									Title:    titleStr,
									Location: locationStr,
								}
							}
							return
						}
					}
				}
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				findJobs(url, c)
			}
		}

		url := fmt.Sprintf("https://%s.bamboohr.com/jobs/", company)

		req, err := http.NewRequest("GET", url, nil)

		if err != nil {
			return
		}

		req = req.WithContext(ctx)

		resp, err := HTTPClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()

		doc, err := html.Parse(resp.Body)

		if err != nil {
			return
		}

		findJobs(url, doc)
	}()

	return jobPostings, nil
}
