package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetDuoLingoJobPostings(t *testing.T) {
	jobPostings, err := GetDuoLingoJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
