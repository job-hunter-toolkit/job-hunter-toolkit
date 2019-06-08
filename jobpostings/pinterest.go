package jobpostings

import (
	"context"
)

// GetPinterestJobPostings finds JobPostings found at https://greenhouse.io
func GetPinterestJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "pinterest")
}
