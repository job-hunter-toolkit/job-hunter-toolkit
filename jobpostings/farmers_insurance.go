package jobpostings

import (
	"context"
)

// GetFarmersInsuranceJobPostings finds JobPostings found at https:/jibeapply.com
func GetFarmersInsuranceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "farmersinsurance")
}
