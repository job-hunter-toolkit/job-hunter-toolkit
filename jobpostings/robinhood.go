package jobpostings

import (
	"context"
)

// GetRobinhoodJobPostings finds JobPostings found at https://greenhouse.io
func GetRobinhoodJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "robinhood")
}
