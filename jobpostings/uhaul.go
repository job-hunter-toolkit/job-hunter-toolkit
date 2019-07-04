package jobpostings

import (
	"context"
)

// GetUhaulJobPostings finds JobPostings using https://uhaul.wd1.myworkdayjobs.com/UhaulJobs
func GetUhaulJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://uhaul.wd1.myworkdayjobs.com/UhaulJobs")
}
