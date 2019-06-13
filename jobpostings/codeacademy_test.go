package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetCodecademyJobPostings(t *testing.T) {
	jobPostings, err := GetCodecademyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
