package jobpostings

import (
	"context"
)

// GetZeplinJobPostings finds JobPostings found at https:/lever.co
func GetZeplinJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "zeplin")
}
