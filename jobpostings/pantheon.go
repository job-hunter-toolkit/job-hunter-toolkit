package jobpostings

import (
	"context"
)

// GetPantheonJobPostings finds JobPostings found at https://greenhouse.io
func GetPantheonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "pantheon")
}
