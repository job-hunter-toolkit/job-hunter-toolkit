package jobpostings

import (
	"context"
)

// GetTTTStudiosJobPostings finds JobPostings found at https://tttstudios.bamboohr.com/jobs/
func GetTTTStudiosJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "tttstudios")
}
