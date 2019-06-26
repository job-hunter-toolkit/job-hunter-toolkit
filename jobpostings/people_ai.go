package jobpostings

import (
	"context"
)

// GetPeopleAIJobPostings finds JobPostings found at https:/lever.co
func GetPeopleAIJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "people-ai")
}
