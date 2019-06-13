package jobpostings

import (
	"context"
	"fmt"
	"testing"
)

func TestGetSproutSocialobPostings(t *testing.T) {
	jobPostings, err := GetSproutSocialobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
