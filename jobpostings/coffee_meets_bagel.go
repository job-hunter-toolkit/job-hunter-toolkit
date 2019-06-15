package jobpostings

import (
	"context"
)

// GetCoffeeMeetsBagelJobPostings finds JobPostings found at https:/lever.co
func GetCoffeeMeetsBagelJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getLeverJobsFor(context.Background(), "coffeemeetsbagel")
}
