package jobpostings

import (
	"context"
)

// GetLiltJobPostings finds JobPostings found at https:/lever.co
func GetLiltJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "lilt")
}
