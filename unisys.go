package jabba

import (
	"context"
)

// GetUnisysJobPostings finds JobPostings using https://unisys.wd5.myworkdayjobs.com/External
func GetUnisysJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://unisys.wd5.myworkdayjobs.com/External")
}