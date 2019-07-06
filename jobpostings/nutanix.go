package jobpostings

import (
	"context"
)

// GetNutanixJobPostings finds JobPostings found at https://jobvite.com
func GetNutanixJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(ctx, "nutanix")
}
