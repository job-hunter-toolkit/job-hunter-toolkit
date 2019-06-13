package jobpostings

import (
	"context"
)

// GetRemarkablyJobPostings finds JobPostings found at https://greenhouse.io
func GetRemarkablyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "remarkably")
}
