package jobpostings

import (
	"context"
)

// GetInventablesJobPostings finds JobPostings found at https://greenhouse.io
func GetInventablesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "inventables")
}
