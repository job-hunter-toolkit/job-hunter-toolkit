package jobpostings

import (
	"context"
)

// GetPagerDutyJobPostings finds JobPostings found at https:/lever.co
func GetPagerDutyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "pagerduty")
}
