package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBoomSupersonicJobPostings(t *testing.T) {
	jobPostings, err := GetBoomSupersonicJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}