package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSkillshareJobPostings(t *testing.T) {
	jobPostings, err := GetSkillshareJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}