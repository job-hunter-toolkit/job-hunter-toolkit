package jobpostings

import (
	"context"
)

// GetWheelsJobPostings finds JobPostings found at https:/lever.co
func GetWheelsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "getwheelsapp")
}
