package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSurveyMonkeyJobPostings(t *testing.T) {
	jobPostings, err := GetSurveyMonkeyJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
