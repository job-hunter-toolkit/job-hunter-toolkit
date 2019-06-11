package jobpostings

import (
	"context"
)

// GetPsiKickJobPostings finds JobPostings found at https://greenhouse.io
func GetPsiKickJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "psikick")
}
