package jobpostings

import (
	"context"
)

// GetKayakJobPostings finds JobPostings found at https://greenhouse.io
func GetKayakJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "kayak")
}
