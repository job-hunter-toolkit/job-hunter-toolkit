package jobpostings

import (
	"context"
)

// GetGradleJobPostings finds JobPostings found at https://greenhouse.io
func GetGradleJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "gradle")
}
