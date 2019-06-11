package jobpostings

import (
	"context"
)

// GetFastlyJobPostings finds JobPostings found at https://greenhouse.io
func GetFastlyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "fastly")
}
