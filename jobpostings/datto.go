package jobpostings

import (
	"context"
)

// GetDattoJobPostings finds JobPostings using https://greenhouse.io
func GetDattoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "datto")
}
