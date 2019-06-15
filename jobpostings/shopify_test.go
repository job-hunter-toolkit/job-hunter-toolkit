package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetShopifyJobPostings(t *testing.T) {
	jobPostings, err := GetShopifyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}