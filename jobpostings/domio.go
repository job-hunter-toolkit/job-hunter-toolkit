package jobpostings

import (
	"context"
)

// GetDominoJobPostings finds JobPostings found at https:/lever.co
func GetDominoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "domio")
}
