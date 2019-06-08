package jobpostings

import (
	"context"
)

// GetBuzzFeedJobPostings finds JobPostings found at https://greenhouse.io
func GetBuzzFeedJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "buzzfeed")
}
