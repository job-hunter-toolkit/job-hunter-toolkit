package jobpostings

import (
	"context"
)

// Get15FiveJobPostings finds JobPostings found at https:/lever.co
func Get15FiveJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "15five")
}
