package jobpostings

import (
	"context"
)

// GetGovPredictJobPostings finds JobPostings found at https://greenhouse.io
func GetGovPredictJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "govpredict")
}
