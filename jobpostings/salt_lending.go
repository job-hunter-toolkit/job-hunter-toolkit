package jobpostings

import (
	"context"
)

// GetSaltLendingJobPostings finds JobPostings found at https://greenhouse.io
func GetSaltLendingJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "saltlending")
}
