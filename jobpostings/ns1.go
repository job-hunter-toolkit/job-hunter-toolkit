package jobpostings

import (
	"context"
)

// GetNS1JobPostings finds JobPostings found at https://greenhouse.io
func GetNS1JobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "ns1")
}
