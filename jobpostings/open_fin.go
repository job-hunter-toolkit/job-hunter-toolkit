package jobpostings

import (
	"context"
)

// GetOpenFinJobPostings finds JobPostings found at https://greenhouse.io
func GetOpenFinJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "openfin95")
}
