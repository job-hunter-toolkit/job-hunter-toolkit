package jobpostings

import (
	"context"
)

// GetNarvarJobPostings finds JobPostings found at https://greenhouse.io
func GetNarvarJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "narvar")
}
