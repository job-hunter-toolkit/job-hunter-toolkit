package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetUnity3DJobPostings(t *testing.T) {
	jobPostings, err := GetUnity3DJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
