package jobpostings

import (
	"context"
)

// GetTaplyticsJobPostings finds JobPostings found at https:/lever.co
func GetTaplyticsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "taplytics")
}
