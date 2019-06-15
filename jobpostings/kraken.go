package jobpostings

import (
	"context"
)

// GetKrakenJobPostings finds JobPostings found at https:/lever.co
func GetKrakenJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "kraken")
}
