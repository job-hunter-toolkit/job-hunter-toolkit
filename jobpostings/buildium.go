package jobpostings

import (
	"context"
)

// GetBuildiumJobPostings finds JobPostings found at https://greenhouse.io
func GetBuildiumJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "buildium")
}
