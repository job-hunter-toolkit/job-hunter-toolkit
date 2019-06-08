package jabba

import (
	"context"
)

// GetMajorLeagueBaseballPostings finds JobPostings found at https://greenhouse.io
func GetMajorLeagueBaseballPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "majorleaguebaseball")
}
