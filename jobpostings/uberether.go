package jobpostings

import (
	"context"
)

// GetUberEtherJobPostings finds JobPostings using https://greenhouse.io
func GetUberEtherJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "uberether")
}
