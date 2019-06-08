package jobpostings

import (
	"context"
)

// GetInstacartJobPostings finds JobPostings found at https://greenhouse.io
func GetInstacartJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "instacart")
}
