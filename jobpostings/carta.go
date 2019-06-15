package jobpostings

import (
	"context"
)

// GetCartaJobPostings finds JobPostings found at https:/lever.co
func GetCartaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "carta")
}
