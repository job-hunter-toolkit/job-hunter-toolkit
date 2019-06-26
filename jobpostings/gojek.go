package jobpostings

import (
	"context"
)

// GetGojekJobPostings finds JobPostings found at https:/lever.co
func GetGojekJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "gojek")
}
