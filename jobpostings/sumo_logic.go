package jobpostings

import (
	"context"
)

// GetSumoLogicJobPostings finds JobPostings found at https://greenhouse.io
func GetSumoLogicJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "sumologic")
}
