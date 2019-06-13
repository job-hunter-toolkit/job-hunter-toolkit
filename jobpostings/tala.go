package jobpostings

import (
	"context"
)

// GetTalaJobPostings finds JobPostings found at https://greenhouse.io
func GetTalaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "tala")
}
