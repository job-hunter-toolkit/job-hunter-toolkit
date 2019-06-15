package jobpostings

import (
	"context"
)

// GetMuxJobPostings finds JobPostings using https://greenhouse.io
func GetMuxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "mux")
}
