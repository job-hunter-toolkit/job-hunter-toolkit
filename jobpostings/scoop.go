package jobpostings

import (
	"context"
)

// GetScoopJobPostings finds JobPostings found at https:/lever.co
func GetScoopJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "takescoop")
}
