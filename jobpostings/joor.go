package jobpostings

import (
	"context"
)

// GetJoorJobPostings finds JobPostings found at https://greenhouse.io
func GetJoorJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "joor")
}
