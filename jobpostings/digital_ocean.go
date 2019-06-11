package jobpostings

import (
	"context"
)

// GetDigitalOceanJobPostings finds JobPostings found at https://greenhouse.io
func GetDigitalOceanJobPostings(ctx context.Context) (<-chan *JobPosting, error) {
	return getGreenHouseJobsFor(ctx, "digitalocean98")
}
