package jobpostings

import (
	"context"
)

// GetSecondSpectrumJobPostings finds JobPostings found at https:/lever.co
func GetSecondSpectrumJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "secondspectrum")
}
