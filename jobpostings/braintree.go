package jobpostings

import (
	"context"
)

// GetBraintreeJobPostings finds JobPostings found at https://greenhouse.io
func GetBraintreeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "braintree")
}
