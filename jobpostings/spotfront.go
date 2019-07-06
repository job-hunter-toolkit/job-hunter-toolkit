package jobpostings

import (
	"context"
)

// GetSpotfrontJobPostings finds JobPostings found at https://greenhouse.io
func GetSpotfrontJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "spotfront")
}
