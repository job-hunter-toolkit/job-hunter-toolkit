package jobpostings

import (
	"context"
)

// GetZipwhipJobPostings finds JobPostings found at https://greenhouse.io
func GetZipwhipJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "zipwhip")
}
