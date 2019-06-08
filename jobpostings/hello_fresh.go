package jobpostings

import (
	"context"
)

// GetHelloFreshJobPostings finds JobPostings found at https://greenhouse.io
func GetHelloFreshJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "hellofresh")
}
