package jobpostings

import (
	"context"
)

// GetLogDNAJobPostings finds JobPostings using https://greenhouse.io
func GetLogDNAJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "logdna")
}
