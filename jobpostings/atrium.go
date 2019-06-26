package jobpostings

import (
	"context"
)

// GetAtriumJobPostings finds JobPostings found at https:/lever.co
func GetAtriumJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "atrium")
}
