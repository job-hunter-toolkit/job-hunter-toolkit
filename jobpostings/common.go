package jobpostings

import (
	"context"
)

// GetCommonJobPostings finds JobPostings found at https://greenhouse.io
func GetCommonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "common")
}
