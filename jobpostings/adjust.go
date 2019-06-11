package jobpostings

import (
	"context"
)

// GetAdjustJobPostings finds JobPostings found at https://greenhouse.io
func GetAdjustJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "adjust")
}
