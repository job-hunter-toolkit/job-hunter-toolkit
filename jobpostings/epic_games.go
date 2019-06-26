package jobpostings

import (
	"context"
)

// GetEpicGamesJobPostings finds JobPostings found at https://epicgames.wd5.myworkdayjobs.com/en-US/Epic_Games
func GetEpicGamesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://epicgames.wd5.myworkdayjobs.com/en-US/Epic_Games")
}
