package jobpostings

import (
	"context"
)

// GetBackerKitJobPostings finds JobPostings found at https:/lever.co
func GetBackerKitJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "backerkit")
}
