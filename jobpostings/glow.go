package jobpostings

import (
	"context"
)

// GetGlowJobPostings finds JobPostings found at https://greenhouse.io
func GetGlowJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "glow")
}
