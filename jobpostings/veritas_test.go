package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetVeritasJobPostings(t *testing.T) {
	jobPostings, err := GetVeritasJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
