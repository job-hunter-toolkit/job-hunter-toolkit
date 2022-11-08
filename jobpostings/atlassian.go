package jobpostings

import (
	"context"
)

// GetAtlassianJobPostings finds JobPostings found at https://jobs.lever.co/atlassian
func GetAtlassianJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "atlassian")
}
