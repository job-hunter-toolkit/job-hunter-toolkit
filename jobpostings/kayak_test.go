package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetKayakJobPostings(t *testing.T) {
	jobPostings, err := GetKayakJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}