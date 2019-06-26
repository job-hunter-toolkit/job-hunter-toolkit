package jobpostings

import (
	"context"
)

// GetKariusJobPostings finds JobPostings found at https:/lever.co
func GetKariusJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "kariusdx")
}
