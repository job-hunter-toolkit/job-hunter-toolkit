package jobpostings

import (
	"context"
)

// GetUpstartJobPostings finds JobPostings found at https://greenhouse.io
func GetUpstartJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "upstart")
}
