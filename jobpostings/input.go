package jobpostings

import (
	"context"
)

// GetInputJobPostings finds JobPostings found at https://greenhouse.io
func GetInputJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "input")
}
