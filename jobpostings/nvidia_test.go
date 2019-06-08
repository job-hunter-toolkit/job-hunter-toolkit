package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetNvidiaJobPostings(t *testing.T) {
	jobPostings, err := GetNvidiaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
