package jobpostings

import (
	"context"
)

// GetStripeJobPostings finds JobPostings found at https://greenhouse.io
func GetStripeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "stripe")
}
