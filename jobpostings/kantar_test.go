package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetKantarJobPostings(t *testing.T) {
	jobPostings, err := GetKantarJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
