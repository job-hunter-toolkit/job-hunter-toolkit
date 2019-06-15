package jobpostings

import (
	"context"
)

// GetBlamelessJobPostings finds JobPostings found at https:/lever.co
func GetBlamelessJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "blameless")
}
