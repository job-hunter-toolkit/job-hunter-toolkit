package jobpostings

import (
	"context"
)

// GetRainforestQAJobPostings finds JobPostings found at https:/lever.co
func GetRainforestQAJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "rainforest")
}
