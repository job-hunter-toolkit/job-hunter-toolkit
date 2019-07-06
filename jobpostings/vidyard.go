package jobpostings

import (
	"context"
)

// GetVidyardJobPostings finds JobPostings found at https://greenhouse.io
func GetVidyardJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "vidyard")
}
