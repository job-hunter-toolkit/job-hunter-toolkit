package jobpostings

import (
	"context"
)

// GetStravaJobPostings finds JobPostings found at https://greenhouse.io
func GetStravaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "strava")
}
