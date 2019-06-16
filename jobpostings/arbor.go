package jobpostings

import (
	"context"
)

// GetArborJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetArborJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(context.Background(), "arborbio")
}
