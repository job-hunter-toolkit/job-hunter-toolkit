package jobpostings

import (
	"context"
)

// GetSensorTowerJobPostings finds JobPostings found at https:/lever.co
func GetSensorTowerJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "sensortower")
}
