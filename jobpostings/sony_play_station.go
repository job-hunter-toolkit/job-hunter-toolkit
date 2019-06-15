package jobpostings

import (
	"context"
)

// GetSonyPlayStationJobPostings finds JobPostings using https://greenhouse.io
func GetSonyPlayStationJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "sonyinteractiveentertainmentplaystation")
}
