package jobpostings

import (
	"context"
)

// GetCodacyJobPostings finds JobPostings found at https:/lever.co
func GetCodacyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "codacy")
}
