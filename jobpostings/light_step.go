package jobpostings

import (
	"context"
)

// GetLightStepJobPostings finds JobPostings found at https://greenhouse.io
func GetLightStepJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "lightstep")
}
