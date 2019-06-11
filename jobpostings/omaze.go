package jobpostings

import (
	"context"
)

// GetOmazeJobPostings finds JobPostings found at https://greenhouse.io
func GetOmazeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "omaze")
}
