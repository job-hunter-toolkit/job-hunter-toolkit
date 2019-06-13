package jobpostings

import (
	"context"
)

// GetSimpliSafeJobPostings finds JobPostings found at https://greenhouse.io
func GetSimpliSafeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "simplisafe")
}
