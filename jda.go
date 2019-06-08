package jabba

import (
	"context"
)

// GetJDAJobPostings finds JobPostings using https://jda.wd5.myworkdayjobs.com/en-US/JDA_Careers
func GetJDAJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://jda.wd5.myworkdayjobs.com/en-US/JDA_Careers")
}