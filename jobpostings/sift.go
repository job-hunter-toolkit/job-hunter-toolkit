package jobpostings

import (
	"context"
)

// GetSiftJobPostings finds JobPostings found at https://greenhouse.io
func GetSiftJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "siftscience")
}
