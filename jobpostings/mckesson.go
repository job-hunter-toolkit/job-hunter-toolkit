package jobpostings

import (
	"context"
)

// GetMcKessonJobPostings finds JobPostings using https://mckesson.wd3.myworkdayjobs.com/External_Careers
func GetMcKessonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://mckesson.wd3.myworkdayjobs.com/External_Careers")
}