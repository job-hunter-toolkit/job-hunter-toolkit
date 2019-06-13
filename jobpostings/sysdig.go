package jobpostings

import (
	"context"
)

// GetSysdigJobPostings finds JobPostings found at https://greenhouse.io
func GetSysdigJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "sysdig")
}
