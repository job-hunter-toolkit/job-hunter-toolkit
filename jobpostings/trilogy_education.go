package jobpostings

import (
	"context"
)

// GetTrilogyEducationJobPostings finds JobPostings found at https://greenhouse.io
func GetTrilogyEducationJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "trilogyeducation")
}
