package jobpostings

import (
	"context"
)

// GetCockroachLabsJobPostings finds JobPostings found at https://greenhouse.io
func GetCockroachLabsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "cockroachlabs")
}
