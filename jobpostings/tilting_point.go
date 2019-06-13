package jobpostings

import (
	"context"
)

// GetTiltingPointJobPostings finds JobPostings found at https://greenhouse.io
func GetTiltingPointJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "tiltingpoint")
}
