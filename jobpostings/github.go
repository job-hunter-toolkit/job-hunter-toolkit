package jobpostings

import (
	"context"
)

// GetGitHubJobPostings finds JobPostings found at https://greenhouse.io
func GetGitHubJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "github")
}
