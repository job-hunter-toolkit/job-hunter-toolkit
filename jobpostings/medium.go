package jobpostings

import (
	"context"
)

// GetMediumJobPostings finds JobPostings found at https:/lever.co
func GetMediumJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "medium")
}
