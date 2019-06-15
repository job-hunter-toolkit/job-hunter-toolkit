package jobpostings

import (
	"context"
)

// GetDoorDashJobPostings finds JobPostings using https://greenhouse.io
func GetDoorDashJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "doordash")
}
