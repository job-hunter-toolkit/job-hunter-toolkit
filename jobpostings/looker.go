package jobpostings

import (
	"context"
)

// GetLookerJobPostings finds JobPostings found at https:/lever.co
func GetLookerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "looker")
}
