package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNexTravelobPostings(t *testing.T) {
	jobPostings, err := GetNexTravelobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}