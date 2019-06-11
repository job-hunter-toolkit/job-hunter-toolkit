package jobpostings

import (
	"context"
)

// GetFarmersBusinessNetworkJobPostings finds JobPostings found at https://greenhouse.io
func GetFarmersBusinessNetworkJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "farmersbusinessnetwork")
}

