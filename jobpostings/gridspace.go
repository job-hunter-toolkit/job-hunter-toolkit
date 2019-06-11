package jobpostings

import (
	"context"
)

// GetGridspaceJobPostings finds JobPostings found at https://greenhouse.io
func GetGridspaceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "gridspace")
}
