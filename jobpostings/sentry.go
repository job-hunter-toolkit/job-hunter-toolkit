package jobpostings

import (
	"context"
)

// GetSentryJobPostings finds JobPostings found at https://greenhouse.io
func GetSentryJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "sentry")
}
