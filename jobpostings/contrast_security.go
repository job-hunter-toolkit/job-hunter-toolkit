package jobpostings

import (
	"context"
)

// GetContrastSecurityJobPostings finds JobPostings found at https:/lever.co
func GetContrastSecurityJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "contrastsecurity")
}
