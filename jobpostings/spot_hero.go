package jobpostings

import (
	"context"
)

// GetSpotHeroJobPostings finds JobPostings found at https://greenhouse.io
func GetSpotHeroJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "spothero")
}
