package jobpostings

import (
	"context"
)

// GetScanditJobPostings finds JobPostings found at https://greenhouse.io
func GetScanditJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "scandit")
}
