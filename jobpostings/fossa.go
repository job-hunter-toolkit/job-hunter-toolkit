package jobpostings

import (
	"context"
)

// GetFossaJobPostings finds JobPostings found at https:/lever.co
func GetFossaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "fossa")
}
