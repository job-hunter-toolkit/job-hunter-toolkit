package jobpostings

import (
	"context"
)

// GetResearchGateJobPostings finds JobPostings found at https:/lever.co
func GetResearchGateJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "researchgate")
}
