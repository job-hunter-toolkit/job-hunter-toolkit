package jobpostings

import (
	"context"
)

// GetTellerJobPostings finds JobPostings found at https://jobs.lever.co/teller
func GetTellerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "teller")
}
