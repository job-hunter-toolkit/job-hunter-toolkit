package jobpostings

import (
	"context"
)

// GetBeamlyJobPostings finds JobPostings found at https://greenhouse.io
func GetBeamlyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "beamly")
}
