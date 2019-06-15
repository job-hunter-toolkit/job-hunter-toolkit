package jobpostings

import (
	"context"
)

// GetCobaltRoboticsJobPostings finds JobPostings found at https:/lever.co
func GetCobaltRoboticsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "cobaltrobotics")
}
