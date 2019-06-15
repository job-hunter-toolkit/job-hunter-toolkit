package jobpostings

import (
	"context"
)

// GetAmenityAnalyticsJobPostings finds JobPostings found at https:/lever.co
func GetAmenityAnalyticsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "amenityanalytics")
}
