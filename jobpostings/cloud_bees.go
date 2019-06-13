package jobpostings

import (
	"context"
)

// GetCloudBeesJobPostings finds JobPostings found at https://greenhouse.io
func GetCloudBeesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "cloudbees")
}
