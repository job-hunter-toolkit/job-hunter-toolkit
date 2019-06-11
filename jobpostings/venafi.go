package jobpostings

import (
	"context"
)

// GetVenafiJobPostings finds JobPostings found at https://greenhouse.io
func GetVenafiJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "venafi")
}
