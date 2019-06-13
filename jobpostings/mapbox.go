package jobpostings

import (
	"context"
)

// GetMapboxJobPostings finds JobPostings found at https://greenhouse.io
func GetMapboxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "mapbox")
}
