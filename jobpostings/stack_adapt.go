package jobpostings

import (
	"context"
)

// GetStackAdaptJobPostings finds JobPostings found at https:/lever.co
func GetStackAdaptJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "stackadapt")
}
