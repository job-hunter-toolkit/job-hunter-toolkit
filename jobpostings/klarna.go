package jobpostings

import (
	"context"
)

// GetKlarnaJobPostings finds JobPostings found at https:/lever.co
func GetKlarnaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "klarna")
}
