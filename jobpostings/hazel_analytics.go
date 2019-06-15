package jobpostings

import (
	"context"
)

// GetHazelAnalyticsJobPostings finds JobPostings found at https:/lever.co
func GetHazelAnalyticsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "hazelanalytics")
}
