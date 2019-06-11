package jobpostings

import (
	"context"
)

// GetBittrexJobPostings finds JobPostings found at https://greenhouse.io
func GetBittrexJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "bittrex")
}
