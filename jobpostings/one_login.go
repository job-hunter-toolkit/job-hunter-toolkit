package jobpostings

import (
	"context"
)

// GetOneLoginJobPostings finds JobPostings found at https://greenhouse.io
func GetOneLoginJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "onelogin")
}
