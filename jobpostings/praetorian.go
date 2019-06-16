package jobpostings

import (
	"context"
)

// GetPraetorianJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetPraetorianJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(context.Background(), "praetoriancom")
}
