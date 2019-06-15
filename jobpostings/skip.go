package jobpostings

import (
	"context"
)

// GetSkipJobPostings finds JobPostings found at https:/lever.co
func GetSkipJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "skipscooters")
}
