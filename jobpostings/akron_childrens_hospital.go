package jobpostings

import (
	"context"
)

// GetAkronChildrensHospitalJobPostings finds JobPostings found at https:/jibeapply.com
func GetAkronChildrensHospitalJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getJibeJobsFor(ctx, "akronchildrenshospital")
}
