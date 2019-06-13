package jobpostings

import (
	"context"
)

// GetGameChangerJobPostings finds JobPostings found at https://greenhouse.io
func GetGameChangerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "gamechanger")
}
