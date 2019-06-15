package jobpostings

import (
	"context"
)

// GetStarskyRoboticsJobPostings finds JobPostings found at https:/lever.co
func GetStarskyRoboticsJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "starskyrobotics")
}
