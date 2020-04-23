package jobpostings

import (
	"context"
)

// GetSonatypeJobPostings finds JobPostings found at https://lever.co
func GetSonatypeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "sonatype")
}
