package jabba

import (
	"context"
)

// GetRiotGamesJobPostings finds JobPostings found at https://greenhouse.io
func GetRiotGamesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "riotgames")
}
