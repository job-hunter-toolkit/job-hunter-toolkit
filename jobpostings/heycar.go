package jobpostings

import (
	"context"
)

// GetHeycarJobPostings finds JobPostings found at https://greenhouse.io
func GetHeycarJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "heycar")
}
