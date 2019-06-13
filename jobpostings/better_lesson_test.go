package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetBetterLessonJobPostings(t *testing.T) {
	jobPostings, err := GetBetterLessonJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}