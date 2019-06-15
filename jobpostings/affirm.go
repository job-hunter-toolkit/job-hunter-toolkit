package jobpostings

import (
	"context"
)

// GetAffirmJobPostings finds JobPostings found at https:/lever.co
func GetAffirmJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "affirm")
}
