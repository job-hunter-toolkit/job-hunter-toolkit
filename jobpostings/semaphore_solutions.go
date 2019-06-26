package jobpostings

import (
	"context"
)

// GetSemaphoreSolutionsJobPostings finds JobPostings found at https:/lever.co
func GetSemaphoreSolutionsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "semaphoresolutions")
}
