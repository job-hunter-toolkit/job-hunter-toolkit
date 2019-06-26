package jobpostings

import (
	"context"
)

// GetBoxLunchJobPostings finds JobPostings found at https:/lever.co
func GetBoxLunchJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "boxlunch")
}
