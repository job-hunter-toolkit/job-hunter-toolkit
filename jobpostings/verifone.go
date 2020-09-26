package jobpostings

import (
	"context"
)

// GetVerifoneJobPostings finds JobPostings found at https://greenhouse.io
func GetVerifoneJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "verifone")
}
