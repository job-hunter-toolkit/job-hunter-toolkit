package jobpostings

import (
	"context"
)

// GetKikJobPostings finds JobPostings found at https://greenhouse.io
func GetKikJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "kik")
}
