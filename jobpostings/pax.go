package jobpostings

import (
	"context"
)

// GetPAXJobPostings finds JobPostings found at https://greenhouse.io
func GetPAXJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "paxlabs")
}
