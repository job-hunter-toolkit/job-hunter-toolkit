package jobpostings

import (
	"context"
)

// GetJellyfishJobPostings finds JobPostings found at https:/lever.co
func GetJellyfishJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "jellyfish")
}
