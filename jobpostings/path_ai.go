package jobpostings

import (
	"context"
)

// GetPathAIJobPostings finds JobPostings found at https://greenhouse.io
func GetPathAIJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "pathai")
}
