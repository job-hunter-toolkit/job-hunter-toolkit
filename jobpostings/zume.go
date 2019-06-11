package jobpostings

import (
	"context"
)

// GetZumeJobPostings finds JobPostings found at https://greenhouse.io
func GetZumeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "zume")
}
