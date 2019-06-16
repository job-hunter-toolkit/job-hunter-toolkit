package jobpostings

import (
	"context"
)

// GetDuoLingoJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetDuoLingoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(context.Background(), "duolingocom")
}
