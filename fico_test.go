package jabba

import "testing"
import "context"
import "fmt"

func TestGetFICOJobPostings(t *testing.T) {
	jobPostings, err := GetFICOJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}