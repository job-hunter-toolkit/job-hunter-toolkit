package jobpostings

import (
	"context"
)

// GetLeverJobPostings finds JobPostings found at https:/lever.co
func GetLeverJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "lever")
}
