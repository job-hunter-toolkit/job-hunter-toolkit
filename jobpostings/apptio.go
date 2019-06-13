package jobpostings

import (
	"context"
)

// GetApptioJobPostings finds JobPostings found at https://greenhouse.io
func GetApptioJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "apptio")
}
