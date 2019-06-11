package jobpostings

import (
	"context"
)

// GetPolicygeniusJobPostings finds JobPostings found at https://greenhouse.io
func GetPolicygeniusJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "policygenius")
}
