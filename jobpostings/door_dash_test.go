package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDoorDashJobPostings(t *testing.T) {
	jobPostings, err := GetDoorDashJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
