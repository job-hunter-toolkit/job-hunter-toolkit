package jobpostings

import (
	"context"
)

// GetCohereJobPostings finds JobPostings found at https://jobs.lever.co/cohere
func GetCohereJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "cohere")
}
