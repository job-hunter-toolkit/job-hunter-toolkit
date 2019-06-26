package jobpostings

import (
	"context"
)

// GetTuftAndNeedleJobPostings finds JobPostings found at https:/lever.co
func GetTuftAndNeedleJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "tuftandneedle")
}
