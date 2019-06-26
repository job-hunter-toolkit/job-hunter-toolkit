package jobpostings

import (
	"context"
)

// GetTopHatJobPostings finds JobPostings found at https:/lever.co
func GetTopHatJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "tophat")
}
