
package jabba

import "testing"
import "context"
import "fmt"

func TestGetERMJobPostings(t *testing.T) {
	jobPostings, err := GetERMJobPostings(context.Background())

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location)
    }
}