package jobpostings

import (
	"context"
)

// GetFarmWiseJobPostings finds JobPostings found at https://hire.withgoogle.com/
func GetFarmWiseJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getHireWithGoogleJobPostingsFor(context.Background(), "farmwiseio")
}
