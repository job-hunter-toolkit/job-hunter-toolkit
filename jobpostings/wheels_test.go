package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetWheelsJobPostings(t *testing.T) {
	jobPostings, err := GetWheelsJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}