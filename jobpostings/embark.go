package jobpostings

import (
	"context"
)

// GetEmbarkJobPostings finds JobPostings found at https:/lever.co
func GetEmbarkJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "embark")
}
