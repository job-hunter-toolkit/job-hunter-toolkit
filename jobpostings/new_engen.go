package jobpostings

import (
	"context"
)

// GetNewEngenJobPostings finds JobPostings found at https://greenhouse.io
func GetNewEngenJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "newengen")
}
