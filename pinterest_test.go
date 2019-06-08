package jabba

import "testing"
import "context"
import "fmt"

func TestGetPinterestJobPostings(t *testing.T) {
	jobPostings, err := GetPinterestJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}