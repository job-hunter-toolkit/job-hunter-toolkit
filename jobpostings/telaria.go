package jobpostings

import (
	"context"
)

// GetTelariaJobPostings finds JobPostings found at https://greenhouse.io
func GetTelariaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "telaria")
}
