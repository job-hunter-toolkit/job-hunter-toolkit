package jobpostings

import (
	"context"
)

// GetQuiddJobPostings finds JobPostings found at https://greenhouse.io
func GetQuiddJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "quidd")
}
