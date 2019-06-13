package jobpostings

import (
	"context"
)

// GetCompassJobPostings finds JobPostings found at https://greenhouse.io
func GetCompassJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "urbancompass")
}
