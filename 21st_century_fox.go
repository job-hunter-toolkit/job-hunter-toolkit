package jabba

import (
	"context"
)

// Get21stCenturyFoxJobPostings finds JobPostings using https://21cf.wd1.myworkdayjobs.com/Domestic
func Get21stCenturyFoxJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://21cf.wd1.myworkdayjobs.com/Domestic")
}