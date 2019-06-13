package jobpostings

import (
	"context"
)

// GetPushpayJobPostings finds JobPostings found at https://greenhouse.io
func GetPushpayJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "pushpay")
}
