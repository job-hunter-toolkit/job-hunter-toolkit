package jobpostings

import (
	"context"
)

// GetZeroCraterJobPostings finds JobPostings found at https://greenhouse.io
func GetZeroCraterJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "zerocater")
}
