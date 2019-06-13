package jobpostings

import (
	"context"
)

// GetMovableInkJobPostings finds JobPostings found at https://greenhouse.io
func GetMovableInkJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "movableink")
}
