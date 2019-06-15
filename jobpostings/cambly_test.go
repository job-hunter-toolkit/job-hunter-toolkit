package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCamblyJobPostings(t *testing.T) {
	jobPostings, err := GetCamblyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}