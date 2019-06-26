package jobpostings

import (
	"context"
)

// GetJDAJobPostings finds JobPostings using https://jda.wd5.myworkdayjobs.com/en-US/JDA_Careers
func GetJDAJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(ctx, "https://jda.wd5.myworkdayjobs.com/en-US/JDA_Careers")
}
