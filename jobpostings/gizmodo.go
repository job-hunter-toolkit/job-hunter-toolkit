package jobpostings

import (
	"context"
)

// GetGizmodoJobPostings finds JobPostings found at https://greenhouse.io
func GetGizmodoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "gizmodomedia")
}
