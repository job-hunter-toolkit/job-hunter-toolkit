package jobpostings

import (
	"context"
)

// GetDotsJobPostings finds JobPostings found at https://greenhouse.io
func GetDotsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "dots")
}
