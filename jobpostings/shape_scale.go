package jobpostings

import (
	"context"
)

// GetShapeScaleCraterJobPostings finds JobPostings found at https://greenhouse.io
func GetShapeScaleCraterJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "shapescale")
}
