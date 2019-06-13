package jobpostings

import (
	"context"
)

// GetWizelineJobPostings finds JobPostings found at https://greenhouse.io
func GetWizelineJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "wizeline")
}
