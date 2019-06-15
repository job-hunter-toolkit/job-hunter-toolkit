package jobpostings

import (
	"context"
)

// GetUserLeapJobPostings finds JobPostings found at https:/lever.co
func GetUserLeapJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "userleap")
}
