package jobpostings

import (
	"context"
)

// GetTutelaJobPostings finds JobPostings found at https://tut.bamboohr.com/jobs/
func GetTutelaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "tut")
}
