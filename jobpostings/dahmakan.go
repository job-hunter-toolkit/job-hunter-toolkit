package jobpostings

import (
	"context"
)

// GetDahmakanJobPostings finds JobPostings using https://greenhouse.io
func GetDahmakanJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "dahmakan")
}
