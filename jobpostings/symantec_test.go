package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSymantecJobPostings(t *testing.T) {
	jobPostings, err := GetSymantecJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
