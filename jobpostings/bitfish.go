package jobpostings

import (
	"context"
)

// GetBitfishJobPostings finds JobPostings found at https:/lever.co
func GetBitfishJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "bit")
}
