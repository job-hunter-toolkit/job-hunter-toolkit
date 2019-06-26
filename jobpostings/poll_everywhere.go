package jobpostings

import (
	"context"
)

// GetPollEverywhereJobPostings finds JobPostings found at https:/lever.co
func GetPollEverywhereJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "polleverywhere")
}
