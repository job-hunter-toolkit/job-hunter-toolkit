package jobpostings

import (
	"context"
)

// GetSpotifyJobPostings finds JobPostings found at https://jobvite.com
func GetSpotifyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(context.Background(), "spotify")
}
