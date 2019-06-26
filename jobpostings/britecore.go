package jobpostings

import (
	"context"
)

// GetBritecoreJobPostings finds JobPostings found at https://britecore.bamboohr.com/jobs/
func GetBritecoreJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "britecore")
}
