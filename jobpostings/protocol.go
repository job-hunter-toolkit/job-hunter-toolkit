package jobpostings

import (
	"context"
)

// GetProtocolJobPostings finds JobPostings found at https:/lever.co
func GetProtocolJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "protocol")
}
