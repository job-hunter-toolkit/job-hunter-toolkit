package jobpostings

import (
	"context"
)

// GetOpendoorJobPostings finds JobPostings found at https:/lever.co
func GetOpendoorJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "opendoor")
}
