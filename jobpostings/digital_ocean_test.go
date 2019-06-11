package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDigitalOceanJobPostings(t *testing.T) {
	jobPostings, err := GetDigitalOceanJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
