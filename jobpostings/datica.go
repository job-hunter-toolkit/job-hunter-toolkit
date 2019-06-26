package jobpostings

import (
	"context"
)

// GetDaticaJobPostings finds JobPostings found at https://datica.bamboohr.com/jobs/
func GetDaticaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(ctx, "datica")
}
