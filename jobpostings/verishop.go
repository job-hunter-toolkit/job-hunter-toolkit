package jobpostings

import (
	"context"
)

// GetVerishopJobPostings finds JobPostings found at https://greenhouse.io
func GetVerishopJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "verishop")
}
