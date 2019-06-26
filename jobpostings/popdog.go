package jobpostings

import (
	"context"
)

// GetPopdogJobPostings finds JobPostings found at https:/lever.co
func GetPopdogJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "popdog")
}
