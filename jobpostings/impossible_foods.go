package jobpostings

import (
	"context"
)

// GetImpossibleFoodsJobPostings finds JobPostings found at https:/lever.co
func GetImpossibleFoodsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "impossiblefoods")
}
