package jobpostings

import (
	"context"
)

// GetSnapRaiseJobPostings finds JobPostings found at https://greenhouse.io
func GetSnapRaiseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "snapraise")
}
