package jobpostings

import (
	"context"
)

// GetDockerJobPostings finds JobPostings found at https://greenhouse.io
func GetDockerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "docker")
}
