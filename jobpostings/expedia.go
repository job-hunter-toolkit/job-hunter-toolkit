package jobpostings

import (
	"context"
)

// GetExpediaJobPostings finds JobPostings using https://expedia.wd5.myworkdayjobs.com/en-US/search
func GetExpediaJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://expedia.wd5.myworkdayjobs.com/en-US/search")
}
