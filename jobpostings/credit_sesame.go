package jobpostings

import (
	"context"
)

// GetCreditSesameJobPostings finds JobPostings found at https:/lever.co
func GetCreditSesameJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "creditsesame")
}
