package jobpostings

import (
	"context"
)

// GetDropJobPostings finds JobPostings found at https://greenhouse.io
func GetDropJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "drop")
}
