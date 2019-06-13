package jobpostings

import (
	"context"
)

// GetCorelightJobPostings finds JobPostings found at https://greenhouse.io
func GetCorelightJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "corelight")
}
