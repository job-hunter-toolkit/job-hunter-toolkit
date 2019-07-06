package jobpostings

import (
	"context"
)

// GetCaliberCollisionJobPostings finds JobPostings found at https:/jibeapply.com
func GetCaliberCollisionJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "calibercollision")
}
