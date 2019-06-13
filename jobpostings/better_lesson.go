package jobpostings

import (
	"context"
)

// GetBetterLessonJobPostings finds JobPostings found at https://betterlesson.bamboohr.com/jobs/
func GetBetterLessonJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getBambooHRJobsFor(context.Background(), "betterlesson")
}
