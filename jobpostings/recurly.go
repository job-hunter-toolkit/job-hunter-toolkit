package jobpostings

import (
	"context"
)

// GetRecurlyJobPostings finds JobPostings found at https:/lever.co
func GetRecurlyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "recurly")
}
