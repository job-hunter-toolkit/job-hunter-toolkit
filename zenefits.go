package jabba

import (
	"context"
)

// GetZenefitsJobPostings finds JobPostings found at https://greenhouse.io
func GetZenefitsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "zenefits")
}
