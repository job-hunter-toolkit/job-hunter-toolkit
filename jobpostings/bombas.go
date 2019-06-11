package jobpostings

import (
	"context"
)

// GetBombasJobPostings finds JobPostings found at https://greenhouse.io
func GetBombasJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "bombas")
}
