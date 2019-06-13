package jobpostings

import (
	"context"
)

// GetGrammarlyJobPostings finds JobPostings found at https://greenhouse.io
func GetGrammarlyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "grammarly")
}
