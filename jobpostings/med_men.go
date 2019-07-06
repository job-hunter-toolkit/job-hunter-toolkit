package jobpostings

import (
	"context"
)

// GetMedMenJobPostings finds JobPostings found at https://greenhouse.io
func GetMedMenJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "medmen")
}
