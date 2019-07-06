package jobpostings

import (
	"context"
)

// GetBenchJobPostings finds JobPostings found at https://greenhouse.io
func GetBenchJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "bench")
}
