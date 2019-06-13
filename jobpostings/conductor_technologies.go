package jobpostings

import (
	"context"
)

// GetConductorTechnologiesJobPostings finds JobPostings found at https://conductortech.bamboohr.com/jobs/
func GetConductorTechnologiesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "conductortech")
}
