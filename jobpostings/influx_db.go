package jobpostings

import (
	"context"
)

// GetInfluxDBJobPostings finds JobPostings using https://greenhouse.io
func GetInfluxDBJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "influxdb")
}
