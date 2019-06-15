package jobpostings

import (
	"context"
)

// GetClickTimeJobPostings finds JobPostings found at https:/lever.co
func GetClickTimeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "clicktime")
}
