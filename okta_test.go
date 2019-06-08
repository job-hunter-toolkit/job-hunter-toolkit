package jabba

import "testing"
import "context"
import "fmt"

func TestGetOktaJobPostings(t *testing.T) {
	jobPostings, err := GetOktaJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}