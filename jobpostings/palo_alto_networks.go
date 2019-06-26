package jobpostings

import (
	"context"
)

// GetPaloAltoNetworksJobPostings finds JobPostings found at https://jobvite.com
func GetPaloAltoNetworksJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJobviteJobsFor(ctx, "paloaltonetworks")
}
