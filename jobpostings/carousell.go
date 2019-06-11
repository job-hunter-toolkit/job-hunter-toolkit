package jobpostings

import (
	"context"
)

// GetCarousellJobPostings finds JobPostings found at https://greenhouse.io
func GetCarousellJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "carousell")
}
