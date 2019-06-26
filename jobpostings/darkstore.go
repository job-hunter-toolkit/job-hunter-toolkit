package jobpostings

import (
	"context"
)

// GetDarkstoreJobPostings finds JobPostings found at https:/lever.co
func GetDarkstoreJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "darkstore")
}
