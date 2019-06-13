package jobpostings

import (
	"context"
)

// GetPostmatesJobPostings finds JobPostings found at https://greenhouse.io
func GetPostmatesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "postmates")
}
