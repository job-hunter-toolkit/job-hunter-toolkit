package jabba

import (
	"context"
)

// GetKantarJobPostings finds JobPostings using https://kantar.wd3.myworkdayjobs.com/en-US/KANTAR
func GetKantarJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://kantar.wd3.myworkdayjobs.com/en-US/KANTAR")
}