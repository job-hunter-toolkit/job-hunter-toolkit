package jobpostings

import (
	"context"
)

// GetMakeSpaceJobPostings finds JobPostings using https://greenhouse.io
func GetMakeSpaceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "makespace")
}
