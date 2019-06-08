package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetNationwideJobPostings(t *testing.T) {
	jobPostings, err := GetNationwideJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
