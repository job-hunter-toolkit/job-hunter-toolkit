package jobpostings

import (
	"context"
)

// GetGustoJobPostings finds JobPostings found at https://greenhouse.io
func GetGustoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "gusto")
}
