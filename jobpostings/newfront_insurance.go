package jobpostings

import (
	"context"
)

// GetNewfrontInsuranceJobPostings finds JobPostings found at https:/lever.co
func GetNewfrontInsuranceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "newfrontinsurance")
}
