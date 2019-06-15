package jobpostings

import (
	"context"
)

// GetGeckoboardJobPostings finds JobPostings found at https:/lever.co
func GetGeckoboardJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "geckoboard")
}
