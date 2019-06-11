package jobpostings

import (
	"context"
)

// GetViceJobPostings finds JobPostings found at https://greenhouse.io
func GetViceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "vice")
}
