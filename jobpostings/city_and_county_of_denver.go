package jobpostings

import (
	"context"
)

// GetCityAndCountyOfDenverJobPostings finds JobPostings using https://denver.wd1.myworkdayjobs.com/CCD-denver-denvergov-CSC_Jobs-Civil_service_jobs-Police_Jobs-Fire_Jobs
func GetCityAndCountyOfDenverJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getWorkdayJobPostings(context.Background(), "https://denver.wd1.myworkdayjobs.com/CCD-denver-denvergov-CSC_Jobs-Civil_service_jobs-Police_Jobs-Fire_Jobs")
}
