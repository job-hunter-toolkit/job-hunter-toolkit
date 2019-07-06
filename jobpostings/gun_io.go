package jobpostings

import (
	"context"
)

// GetGunIoJobPostings finds JobPostings found at https:/lever.co
func GetGunIoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "gunio")
}
