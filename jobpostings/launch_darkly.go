package jobpostings

import (
	"context"
)

// GetLaunchDarklyJobPostings finds JobPostings found at https:/lever.co
func GetLaunchDarklyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "launchdarkly")
}
