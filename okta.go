package jabba

import (
	"context"
)

// GetOktaJobPostings finds JobPostings found at https://greenhouse.io
func GetOktaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "okta")
}
