package jobpostings

import (
	"context"
)

// GetPathlightJobPostings finds JobPostings found at https://greenhouse.io
func GetPathlightJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "pathlight")
}
