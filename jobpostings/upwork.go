package jobpostings

import (
	"context"
)

// GetUpworkJobPostings finds JobPostings found at https:/lever.co
func GetUpworkJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "upwork")
}
