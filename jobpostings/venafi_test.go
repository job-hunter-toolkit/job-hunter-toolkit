package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetVenafiJobPostings(t *testing.T) {
	jobPostings, err := GetVenafiJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
