package jobpostings

import (
	"context"
)

// GetNexientJobPostings finds JobPostings found at https://greenhouse.io
func GetNexientJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "nexient")
}
