package jobpostings

import (
	"context"
)

// GetLimeJobPostings finds JobPostings found at https:/lever.co
func GetLimeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "limebike")
}
