package jobpostings

import (
	"context"
)

// GetGitLabJobPostings finds JobPostings found at https://greenhouse.io
func GetGitLabJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "gitlab")
}
