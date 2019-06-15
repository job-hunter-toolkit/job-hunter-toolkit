package jobpostings

import (
	"context"
)

// GetSnapTravelJobPostings finds JobPostings found at https:/lever.co
func GetSnapTravelJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "snaptravel")
}
