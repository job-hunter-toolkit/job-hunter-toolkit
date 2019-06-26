package jobpostings

import (
	"context"
)

// GetOpenAIJobPostings finds JobPostings found at https:/lever.co
func GetOpenAIJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "openai")
}
