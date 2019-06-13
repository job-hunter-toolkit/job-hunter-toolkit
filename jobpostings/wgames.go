package jobpostings

import (
	"context"
)

// GetWGamesJobPostings finds JobPostings found at https://wgames.bamboohr.com/jobs/
func GetWGamesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "wgames")
}
