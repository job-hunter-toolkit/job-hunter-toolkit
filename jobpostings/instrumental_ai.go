package jobpostings

import (
	"context"
)

// GetInstrumentalAIJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetInstrumentalAIJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(ctx, "instrumentalai")
}
