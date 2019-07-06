package jobpostings

import (
	"context"
)

// GetLendingTreeJobPostings finds JobPostings found at https://greenhouse.io
func GetLendingTreeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "lendingtree")
}
