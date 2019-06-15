package jobpostings

import (
	"context"
)

// GetNovaJobPostings finds JobPostings found at https:/lever.co
func GetNovaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "neednova")
}
