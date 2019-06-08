package jabba

import (
	"context"
)

// GetRedditJobPostings finds JobPostings found at https://greenhouse.io
func GetRedditJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "reddit")
}
