package jobpostings

import (
	"context"
)

// GetNextdoorJobPostings finds JobPostings found at https://greenhouse.io
func GetNextdoorJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "nextdoor")
}
