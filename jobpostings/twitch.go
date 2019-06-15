package jobpostings

import (
	"context"
)

// GetTwitchJobPostings finds JobPostings found at https:/lever.co
func GetTwitchJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "twitch")
}
