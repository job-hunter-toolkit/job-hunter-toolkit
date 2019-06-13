package jobpostings

import (
	"context"
)

// GetMavensJobPostings finds JobPostings found at https://mavensglobal.bamboohr.com/jobs/
func GetMavensJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "mavensglobal")
}
