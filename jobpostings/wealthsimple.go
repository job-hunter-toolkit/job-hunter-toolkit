package jobpostings

import (
	"context"
)

// GetWealthsimpleJobPostings finds JobPostings found at https:/lever.co
func GetWealthsimpleJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "wealthsimple")
}
