package jabba

import "testing"
import "context"
import "fmt"

func TestGetToptalJobPostings(t *testing.T) {
	jobPostings, err := GetToptalJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}