package jobpostings

import (
	"context"
)

// GetAnchorFreeJobPostings finds JobPostings found at https://greenhouse.io
func GetAnchorFreeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "anchorfree")
}
