package jobpostings

import (
	"context"
)

// GetAuth0JobPostings finds JobPostings found at https:/lever.co
func GetAuth0JobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "auth0")
}
