package jobpostings

import (
	"context"
)

// GetYelpJobPostings finds JobPostings found at https://jobs.lever.co/yelp
func GetYelpJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "yelp")
}
