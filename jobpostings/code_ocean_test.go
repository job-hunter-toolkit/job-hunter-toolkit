package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCodeOceanJobPostings(t *testing.T) {
	jobPostings, err := GetCodeOceanJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
