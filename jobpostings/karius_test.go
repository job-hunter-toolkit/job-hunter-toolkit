package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetKariusJobPostings(t *testing.T) {
	jobPostings, err := GetKariusJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}