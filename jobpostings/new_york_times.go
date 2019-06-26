package jobpostings

import (
	"context"
)

// GetNewYorkTimesJobPostings finds JobPostings using https://nytimes.wd5.myworkdayjobs.com/en-US/Tech
func GetNewYorkTimesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://nytimes.wd5.myworkdayjobs.com/en-US/Tech")
}
