package jobpostings

import (
	"context"
)

// GetTransparentSystemsJobPostings finds JobPostings found at https:/lever.co
func GetTransparentSystemsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "transparentsystems")
}
