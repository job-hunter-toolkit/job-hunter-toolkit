package jobpostings

import (
	"context"
)

// GetConsensysJobPostings finds JobPostings using https://greenhouse.io
func GetConsensysJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "consensys")
}
