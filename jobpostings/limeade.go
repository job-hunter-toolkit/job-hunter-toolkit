package jobpostings

import (
	"context"
)

// GetLimeadeJobPostings finds JobPostings found at https://greenhouse.io
func GetLimeadeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "limeade")
}
