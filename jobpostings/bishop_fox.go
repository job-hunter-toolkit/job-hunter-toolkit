package jobpostings

import (
	"context"
)

// GetBishopFoxJobPostings finds JobPostings found at https://greenhouse.io
func GetBishopFoxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "bishopfox")
}
