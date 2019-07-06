package jobpostings

import (
	"context"
)

// GetInVisionJobPostings finds JobPostings found at https://greenhouse.io
func GetInVisionJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "invision")
}
