package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetFarmersBusinessNetworkJobPostings(t *testing.T) {
	jobPostings, err := GetFarmersBusinessNetworkJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}