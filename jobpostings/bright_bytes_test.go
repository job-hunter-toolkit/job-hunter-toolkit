package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBrightBytesJobPostings(t *testing.T) {
	jobPostings, err := GetBrightBytesJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
