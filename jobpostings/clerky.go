package jobpostings

import (
	"context"
)

// GetClerkyJobPostings finds JobPostings found at https:/lever.co
func GetClerkyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "clerky")
}
