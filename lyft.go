package jabba

import (
	"context"
)

// GetLyftJobPostings finds JobPostings found at https://greenhouse.io
func GetLyftJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "lyft")
}
