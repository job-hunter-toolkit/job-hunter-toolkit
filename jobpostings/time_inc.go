package jobpostings

import (
	"context"
)

// GetTimeIncJobPostings finds JobPostings found at https://greenhouse.io
func GetTimeIncJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "timeinc")
}
