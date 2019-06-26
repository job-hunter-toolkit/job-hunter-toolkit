package jobpostings

import (
	"context"
)

// GetSnowflakeJobPostings finds JobPostings found at https:/lever.co
func GetSnowflakeJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(ctx, "snowflake")
}
