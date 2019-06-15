package jobpostings

import (
	"context"
)

// GetQuartzyJobPostings finds JobPostings found at https:/lever.co
func GetQuartzyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "quartzy")
}
