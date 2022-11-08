package jobpostings

import (
	"context"
)

// GetBitriseJobPostings finds JobPostings found at https://www.bitrise.io/careers
func GetBitriseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "bitrise")
}
