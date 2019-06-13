package jobpostings

import (
	"context"
)

// GetLighthouseStudiosJobPostings finds JobPostings found at https://lighthouse.bamboohr.com/jobs/
func GetLighthouseStudiosJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "lighthouse")
}
