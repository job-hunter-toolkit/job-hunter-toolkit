package jobpostings

import (
	"context"
)

// GetTimeJobPostings finds JobPostings using https://greenhouse.io
func GetTimeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "timeinc")
}
