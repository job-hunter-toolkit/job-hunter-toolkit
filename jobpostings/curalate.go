package jobpostings

import (
	"context"
)

// GetCuralateJobPostings finds JobPostings found at https://greenhouse.io
func GetCuralateJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "curalate")
}
