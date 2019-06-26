package jobpostings

import (
	"context"
)

// GetZeusJobPostings finds JobPostings found at https:/lever.co
func GetZeusJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "zeus")
}
