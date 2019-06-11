package jobpostings

import (
	"context"
)

// GetFirstJobPostings finds JobPostings found at https://greenhouse.io
func GetFirstJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "first")
}
