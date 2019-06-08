package jabba

import "testing"
import "context"
import "fmt"

func TestWorkdayJobPostings(t *testing.T) {
	//jobPostings, err := getWorkdayJobPostings(context.Background(), "https://carbonblack.wd1.myworkdayjobs.com/Life_at_Cb")
	jobPostings, err := getWorkdayJobPostings(context.Background(), "https://carbonblack.wd1.myworkdayjobs.com/Life_at_Cb")

	if err != nil {
		t.Fatal(err)
	}

	for jobPosting := range jobPostings {
		fmt.Println("title:", jobPosting.Title, "location:", jobPosting.Location, "url:", jobPosting.URL)
	}
}
