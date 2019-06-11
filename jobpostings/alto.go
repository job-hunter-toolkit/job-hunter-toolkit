package jobpostings

import (
	"context"
)

// GetAltoJobPostings finds JobPostings found at https://greenhouse.io
func GetAltoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "alto")
}
