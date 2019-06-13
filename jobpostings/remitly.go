package jobpostings

import (
	"context"
)

// GetRemitlyJobPostings finds JobPostings found at https://greenhouse.io
func GetRemitlyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "remitly")
}
