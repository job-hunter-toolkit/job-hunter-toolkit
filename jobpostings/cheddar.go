package jobpostings

import (
	"context"
)

// GetCheddarJobPostings finds JobPostings found at https://greenhouse.io
func GetCheddarJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "cheddar")
}
