package jobpostings

import (
	"context"
)

// GetSauceLabsJobPostings finds JobPostings found at https://greenhouse.io
func GetSauceLabsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "saucelabs")
}
