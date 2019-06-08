package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetNewYorkTimesJobPostings(t *testing.T) {
	jobPostings, err := GetNewYorkTimesJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
