package jobpostings

import (
	"context"
)

// GetPatreonJobPostings finds JobPostings found at https://greenhouse.io
func GetPatreonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "patreon")
}
