package jobpostings

import (
	"context"
)

// GetSlackJobPostings finds JobPostings using https://greenhouse.io
func GetSlackJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "slack")
}
