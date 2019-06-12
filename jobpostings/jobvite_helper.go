package jobpostings

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

func getJobviteJobsFor(ctx context.Context, company string) (<-chan *JobPosting, error) {
	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		var findJobs func(*html.Node)

		findJobs = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "a" {
				for _, a := range n.Attr {
					if a.Key == "href" {
						if strings.Contains(a.Val, "/job/") {
							url := fmt.Sprintf("https://jobs.jobvite.com%s", a.Val)
							titleStr := strings.TrimSpace(n.FirstChild.Data)
							locationStr := strings.Join(strings.Fields(strings.TrimSpace(n.Parent.Parent.FirstChild.NextSibling.NextSibling.NextSibling.FirstChild.Data)), " ")

							if titleStr == "" {
								locationStr = "unknown"
							}

							if locationStr == "" {
								locationStr = "unknown"
							}

							jobPostings <- &JobPosting{
								URL:      url,
								Title:    titleStr,
								Location: locationStr,
							}

							break
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				findJobs(c)
			}
		}

		var noMoreResults func(*html.Node) bool

		noMoreResults = func(n *html.Node) bool {
			if n.Type == html.ElementNode && n.Data == "p" {
				for _, a := range n.Attr {
					if a.Key == "ng-non-bindable" {
						return true
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if noMoreResults(c) {
					return true
				}
			}
			return false
		}

		page := 1

		for {
			// To include job description, simply add
			// ?content=true as a URL param to the request
			req, err := http.NewRequest("GET", fmt.Sprintf("https://jobs.jobvite.com/%s/search?l=&c=&q=&nl=1&p=%d", company, page), nil)

			// "https://jobs.jobvite.com/%s/search?l=&c=&q=&nl=1&p=%d"
			if err != nil {
				break
			}

			req = req.WithContext(ctx)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				break
			}
			defer resp.Body.Close()

			doc, err := html.Parse(resp.Body)

			if err != nil {
				break
			}

			if noMoreResults(doc) {
				break
			}

			findJobs(doc)

			page++
		}
	}()

	return jobPostings, nil
}
