package jobpostings

import (
	"context"
)

// GetMessageBirdJobPostings finds JobPostings found at https://greenhouse.io
func GetMessageBirdJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "messagebird")
}
