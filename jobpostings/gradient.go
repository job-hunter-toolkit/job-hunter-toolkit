package jobpostings

import (
	"context"
)

// GetGradientJobPostings finds JobPostings found at https://greenhouse.io
func GetGradientJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "gradientio")
}
