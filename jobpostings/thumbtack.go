package jobpostings

import (
	"context"
)

// GetThumbtackJobPostings finds JobPostings found at https://greenhouse.io
func GetThumbtackJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "thumbtack")
}
