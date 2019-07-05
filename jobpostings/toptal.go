package jobpostings

import (
	"context"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// GetToptalJobPostings finds JobPostings found at https://www.toptal.com/careers
func GetToptalJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	resp, err := http.Get("https://www.toptal.com/careers")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)

	if err != nil {
		return nil, err
	}

	jobPostings := make(chan *JobPosting)

	company := "toptal"

	go func() {
		defer close(jobPostings)

		var findJobs func(*html.Node)

		findJobs = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "a" {
				for _, a := range n.Attr {
					if a.Key == "href" {
						if strings.HasSuffix(a.Val, "#apply") {
							url := "https://www.toptal.com" + a.Val
							titleStr := strings.TrimSpace(n.Parent.Parent.Parent.Parent.Parent.FirstChild.FirstChild.FirstChild.FirstChild.Data)
							locationStr := strings.TrimSpace(n.Parent.Parent.Parent.FirstChild.NextSibling.LastChild.FirstChild.Data)

							jobPostings <- &JobPosting{
								Company:  company,
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

		findJobs(doc)
	}()

	return jobPostings, nil
}
