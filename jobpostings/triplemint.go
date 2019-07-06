package jobpostings

import (
	"context"
)

// GetTriplemintJobPostings finds JobPostings found at https://greenhouse.io
func GetTriplemintJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "triplemint")
}
