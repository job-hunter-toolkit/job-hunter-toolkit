package jabba

import (
	"context"
)

// GetEpicGamesJobPostings finds JobPostings found at https://epicgames.wd5.myworkdayjobs.com/en-US/Epic_Games
func GetEpicGamesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://epicgames.wd5.myworkdayjobs.com/en-US/Epic_Games")
}