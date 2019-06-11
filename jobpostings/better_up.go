package jobpostings

import (
	"context"
)

// GetBetterUpJobPostings finds JobPostings found at https://greenhouse.io
func GetBetterUpJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "betterup")
}
