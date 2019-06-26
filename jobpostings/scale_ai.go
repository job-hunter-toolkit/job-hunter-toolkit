package jobpostings

import (
	"context"
)

// GetScaleAIJobPostings finds JobPostings found at https:/lever.co
func GetScaleAIJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "scaleai")
}
