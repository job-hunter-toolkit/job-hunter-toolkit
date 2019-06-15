package jobpostings

import (
	"context"
)

// GetNurxJobPostings finds JobPostings using https://greenhouse.io
func GetNurxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "nurx")
}
