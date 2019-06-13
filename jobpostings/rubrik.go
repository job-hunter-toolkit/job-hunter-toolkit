package jobpostings

import (
	"context"
)

// GetRubrikJobPostings finds JobPostings found at https://greenhouse.io
func GetRubrikJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "rubrik")
}
