package jobpostings

import (
	"context"
)

// GetPlumeJobPostings finds JobPostings found at https://greenhouse.io
func GetPlumeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "plume")
}
