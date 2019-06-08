package jabba

import (
	"context"
)

// GetSurveyMonkeyJobPostings finds JobPostings found at https://greenhouse.io
func GetSurveyMonkeyJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "surveymonkey")
}
