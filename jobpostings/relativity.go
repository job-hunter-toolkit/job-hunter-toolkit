package jobpostings

import (
	"context"
)

// GetRelativityJobPostings finds JobPostings found at https:/lever.co
func GetRelativityJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "relativityspace")
}
