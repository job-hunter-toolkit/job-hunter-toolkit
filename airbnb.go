package jabba

import (
	"context"
)

// GetAirbnbJobPostings finds JobPostings using https://greenhouse.io
func GetAirbnbJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "airbnb")
}