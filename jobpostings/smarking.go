package jobpostings

import (
	"context"
)

// GetSmarkingJobPostings finds JobPostings found at https:/lever.co
func GetSmarkingJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "smarking")
}
