package jobpostings

import (
	"context"
)

// GetSurveyGizmoJobPostings finds JobPostings found at https://greenhouse.io
func GetSurveyGizmoJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "surveygizmo")
}
