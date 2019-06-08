package jobpostings

import "testing"
import "context"
import "fmt"

func TestGetSamsungJobPostings(t *testing.T) {
	jobPostings, err := GetSamsungJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}
