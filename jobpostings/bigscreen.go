package jobpostings

import (
	"context"
)

// GetBigscreenJobPostings finds JobPostings found at https:/lever.co
func GetBigscreenJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "bigscreenvr")
}
