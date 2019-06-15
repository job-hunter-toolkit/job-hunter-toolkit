package jobpostings

import (
	"context"
)

// GetLinuxFoundationJobPostings finds JobPostings found at https:/lever.co
func GetLinuxFoundationJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "linuxfoundation.org")
}
