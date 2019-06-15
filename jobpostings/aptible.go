package jobpostings

import (
	"context"
)

// GetAptibleJobPostings finds JobPostings found at https:/lever.co
func GetAptibleJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "aptible")
}
