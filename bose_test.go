package jabba

import "testing"
import "context"
import "fmt"

func TestGetBoseJobPostings(t *testing.T) {
	jobPostings, err := GetBoseJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
	}
}