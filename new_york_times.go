package jabba

import (
	"context"
)

// GetNewYorkTimesJobPostings finds JobPostings using https://nytimes.wd5.myworkdayjobs.com/en-US/Tech
func GetNewYorkTimesJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://nytimes.wd5.myworkdayjobs.com/en-US/Tech")
}