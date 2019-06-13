package jobpostings

import (
	"context"
)

// GetPlatformshJobPostings finds JobPostings found at https://greenhouse.io
func GetPlatformshJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "platformsh")
}
