package jobpostings

import (
	"context"
)

// GetWeb3JobPostings finds JobPostings found at https://web3.bamboohr.com/jobs/
func GetWeb3JobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "web3")
}
