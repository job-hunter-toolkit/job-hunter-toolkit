package jobpostings

import (
	"context"
)

// Get2UJobPostings finds JobPostings found at https://greenhouse.io
func Get2UJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "2u")
}
