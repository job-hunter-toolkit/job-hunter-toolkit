package jobpostings

import (
	"context"
)

// GetPredictiveIndexJobPostings finds JobPostings found at https://greenhouse.io
func GetPredictiveIndexJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "predictiveindex")
}
