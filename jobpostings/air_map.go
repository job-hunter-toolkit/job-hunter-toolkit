package jobpostings

import (
	"context"
)

// GetAirMapJobPostings finds JobPostings found at https://greenhouse.io
func GetAirMapJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "airmap")
}
