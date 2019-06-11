package jobpostings

import (
	"context"
)

// GetCoreScientificJobPostings finds JobPostings found at https://greenhouse.io
func GetCoreScientificJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "corescientific")
}
