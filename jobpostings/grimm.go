package jobpostings

import (
	"context"
)

// GetGrimmJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetGrimmJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(ctx, "grimmcocom")
}
