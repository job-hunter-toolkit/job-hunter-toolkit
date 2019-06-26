package jobpostings

import (
	"context"
)

// GetTranscendJobPostings finds JobPostings found at https:/lever.co
func GetTranscendJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "transcend")
}
