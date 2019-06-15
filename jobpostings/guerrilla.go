package jobpostings

import (
	"context"
)

// GetGuerrillaJobPostings finds JobPostings using https://greenhouse.io
func GetGuerrillaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "guerrilla")
}
