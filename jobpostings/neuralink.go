package jobpostings

import (
	"context"
)

// GetNeuralinkJobPostings finds JobPostings found at https:/lever.co
func GetNeuralinkJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "neuralink")
}
