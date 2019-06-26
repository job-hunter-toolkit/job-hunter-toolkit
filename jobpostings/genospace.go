package jobpostings

import (
	"context"
)

// GetGenospaceJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetGenospaceJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(ctx, "genospacecom")
}
