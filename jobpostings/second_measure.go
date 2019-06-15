package jobpostings

import (
	"context"
)

// GetSecondMeasureJobPostings finds JobPostings using https://greenhouse.io
func GetSecondMeasureJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "secondmeasure")
}
