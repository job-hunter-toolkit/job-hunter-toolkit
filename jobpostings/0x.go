package jobpostings

import (
	"context"
)

// Get0xJobPostings finds JobPostings found at https://greenhouse.io
func Get0xJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "0x")
}
