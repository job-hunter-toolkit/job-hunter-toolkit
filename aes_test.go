package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetAESJobPostings(t *testing.T) {
	jobPostings, err := GetAESJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}