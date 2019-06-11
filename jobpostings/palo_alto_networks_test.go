package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetPaloAltoNetworksJobPostings(t *testing.T) {
	jobPostings, err := GetPaloAltoNetworksJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
