package jobpostings

import (
	"context"
)

// GetExpensifyJobPostings finds JobPostings found at https://greenhouse.io
func GetExpensifyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "expensify")
}
