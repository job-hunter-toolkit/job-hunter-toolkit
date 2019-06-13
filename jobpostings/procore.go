package jobpostings

import (
	"context"
)

// GetProcoreJobPostings finds JobPostings found at https://greenhouse.io
func GetProcoreJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "procore")
}
