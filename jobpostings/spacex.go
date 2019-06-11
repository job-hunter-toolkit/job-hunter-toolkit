package jobpostings

import (
	"context"
)

// GetSpaceXJobPostings finds JobPostings found at https://greenhouse.io
func GetSpaceXJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "spacex")
}
