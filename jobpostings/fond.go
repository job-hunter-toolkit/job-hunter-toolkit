package jobpostings

import (
	"context"
)

// GetFondJobPostings finds JobPostings found at https:/lever.co
func GetFondJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "fond")
}
