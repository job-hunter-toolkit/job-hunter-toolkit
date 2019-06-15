package jobpostings

import (
	"context"
)

// GetIrisAutomationJobPostings finds JobPostings using https://greenhouse.io
func GetIrisAutomationJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "irisautomation")
}
