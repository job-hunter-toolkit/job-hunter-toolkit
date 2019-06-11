package jobpostings

import (
	"context"
)

// GetAnchorageJobPostings finds JobPostings found at https://greenhouse.io
func GetAnchorageJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "anchorage")
}
