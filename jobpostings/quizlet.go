package jobpostings

import (
	"context"
)

// GetQuizletJobPostings finds JobPostings found at https://jobs.lever.co/quizlet
func GetQuizletJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "quizlet-2")
}
