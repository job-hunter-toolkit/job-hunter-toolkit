package jobpostings

import (
	"context"
)

// GetInsightsoftwareJobPostings finds JobPostings using https://greenhouse.io
func GetInsightsoftwareJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "insightsoftware")
}
