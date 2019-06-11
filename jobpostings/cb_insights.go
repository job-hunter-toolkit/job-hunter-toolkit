package jobpostings

import (
	"context"
)

// GetCBInsightsJobPostings finds JobPostings found at https://greenhouse.io
func GetCBInsightsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "cbinsights")
}
