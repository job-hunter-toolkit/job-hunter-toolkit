package jobpostings

import (
	"context"
)

// GetMagicLeapJobPostings finds JobPostings found at https://greenhouse.io
func GetMagicLeapJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "magicleapinc")
}
