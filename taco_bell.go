package jabba

import (
	"context"
)

// GetTacoBellJobPostings finds JobPostings found at https://greenhouse.io
func GetTacoBellJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "tacobell")
}
