package jobpostings

import (
	"context"
)

// GetCodecademyJobPostings finds JobPostings found at https://greenhouse.io
func GetCodecademyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "codeacademy")
}
