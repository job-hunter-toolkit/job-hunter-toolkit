package jobpostings

import (
	"context"
)

// GetChewyJobPostings finds JobPostings found at https://greenhouse.io
func GetChewyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "chewycom")
}
