package jobpostings

import (
	"context"
)

// GetFatLlamaJobPostings finds JobPostings found at https:/lever.co
func GetFatLlamaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "fatllama")
}
