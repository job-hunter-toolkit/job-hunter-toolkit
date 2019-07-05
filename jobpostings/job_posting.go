package jobpostings

// JobPosting is the basic building block of
// the functionality provided by this toolkit.
type JobPosting struct {
	Company  string `json:"company"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Location string `json:"location"`
}
