package jobpostings

import (
	"context"
)

// GetCodeOceanJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetCodeOceanJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(context.Background(), "codeoceancom")
}
