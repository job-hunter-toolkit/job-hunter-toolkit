package jobpostings

import (
	"context"
)

// GetPioneerSquareLabsJobPostings finds JobPostings found at https://greenhouse.io
func GetPioneerSquareLabsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "pioneersquarelabs")
}
