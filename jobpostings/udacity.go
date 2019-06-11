package jobpostings

import (
	"context"
)

// GetUdacityJobPostings finds JobPostings found at https://greenhouse.io
func GetUdacityJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "udacity")
}
