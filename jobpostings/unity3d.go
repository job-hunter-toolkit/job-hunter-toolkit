package jobpostings

import (
	"context"
)

// GetUnity3DJobPostings finds JobPostings found at https://greenhouse.io
func GetUnity3DJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "unity3d")
}
