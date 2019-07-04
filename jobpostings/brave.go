package jobpostings

import (
	"context"
)

// GetBraveJobPostings finds JobPostings found at https://greenhouse.io
func GetBraveJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "brave")
}
