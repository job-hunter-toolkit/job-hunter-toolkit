package jobpostings

import (
	"context"
)

// GetBlackfynnJobPostings finds JobPostings found at https://blackfynn.bamboohr.com/jobs/
func GetBlackfynnJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "blackfynn")
}
