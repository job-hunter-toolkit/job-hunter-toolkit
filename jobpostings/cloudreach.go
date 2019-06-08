package jobpostings

import (
	"context"
)

// GetCloudreachJobPostings finds JobPostings found at https://greenhouse.io
func GetCloudreachJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "cloudreach")
}
