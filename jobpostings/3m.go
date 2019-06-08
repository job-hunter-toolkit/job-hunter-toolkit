package jobpostings

import (
	"context"
)

// Get3MJobPostings finds JobPostings using https://3m.wd1.myworkdayjobs.com/Search
func Get3MJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://3m.wd1.myworkdayjobs.com/Search")
}
