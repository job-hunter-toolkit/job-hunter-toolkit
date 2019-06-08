package jabba

import (
	"context"
)

// GetSquarespaceJobPostings finds JobPostings found at https://greenhouse.io
func GetSquarespaceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "squarespace")
}
