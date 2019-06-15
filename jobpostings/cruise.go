package jobpostings

import (
	"context"
)

// GetCruiseJobPostings finds JobPostings using https://greenhouse.io
func GetCruiseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "cruise")
}
