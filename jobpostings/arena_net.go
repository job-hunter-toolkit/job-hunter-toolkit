package jobpostings

import (
	"context"
)

// GetArenaNetJobPostings finds JobPostings using https://greenhouse.io
func GetArenaNetJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "arenanet")
}
