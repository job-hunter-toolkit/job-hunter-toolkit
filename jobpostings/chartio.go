package jobpostings

import (
	"context"
)

// GetChartioJobPostings finds JobPostings found at https:/lever.co
func GetChartioJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "chartio")
}
