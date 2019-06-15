package jobpostings

import (
	"context"
)

// GetLucidJobPostings finds JobPostings found at https:/lever.co
func GetLucidJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "lucid")
}
