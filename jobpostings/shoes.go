package jobpostings

import (
	"context"
)

// GetShoesJobPostings finds JobPostings found at https://greenhouse.io
func GetShoesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "shoes")
}
