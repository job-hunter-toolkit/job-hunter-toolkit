package jobpostings

import (
	"context"
)

// GetHelloSignJobPostings finds JobPostings found at https:/lever.co
func GetHelloSignJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "hellosign")
}
