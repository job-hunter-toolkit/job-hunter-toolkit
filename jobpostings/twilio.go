package jobpostings

import (
	"context"
)

// GetTwilioJobPostings finds JobPostings found at https://greenhouse.io
func GetTwilioJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "twilio")
}
