package jobpostings

import (
	"context"
)

// GetNianticJobPostings finds JobPostings found at https://greenhouse.io
func GetNianticJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "niantic")
}
