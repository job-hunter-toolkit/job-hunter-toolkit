package jobpostings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type jibeJobs struct {
	Jobs []struct {
		Data struct {
			//Slug         string   `json:"slug"`
			//Language     string   `json:"language"`
			//Languages    []string `json:"languages"`
			//ReqID        string   `json:"req_id"`
			Title string `json:"title"`
			//Description  string   `json:"description"`
			City        string `json:"city"`
			State       string `json:"state"`
			Country     string `json:"country"`
			CountryCode string `json:"country_code"`
			//LocationType string   `json:"location_type"`
			//Latitude     float64  `json:"latitude"`
			//Longitude    float64  `json:"longitude"`
			//Categories   []struct {
			//	Name string `json:"name"`
			//} `json:"categories"`
			//Tags             []string `json:"tags"`
			//ExperienceLevels []string `json:"experience_levels"`
			ApplyURL string `json:"apply_url"`
			//Internal         bool     `json:"internal"`
			//Searchable       bool     `json:"searchable"`
			//Applyable        bool     `json:"applyable"`
			//LiEasyApplyable  bool     `json:"li_easy_applyable"`
			//AtsCode          string   `json:"ats_code"`
			//MetaData         struct {
			//	Ats                        string        `json:"ats"`
			//	AtsInstance                string        `json:"ats_instance"`
			//	FlowConfigName             string        `json:"flow_config_name"`
			//	Gqid                       string        `json:"gqid"`
			//	ImportSource               string        `json:"import_source"`
			//	InternationalCampusPosting string        `json:"international_campus_posting?"`
			//	JobBoards                  []interface{} `json:"job_boards"`
			//	JobID                      string        `json:"job_id"`
			//	LastSynced                 string        `json:"last_synced"`
			//	LocaleID                   string        `json:"locale_id"`
			//	LoginURL                   string        `json:"login_url"`
			//	PartnerID                  int           `json:"partner_id"`
			//	QuestionSets               []struct {
			//		Name    string `json:"name"`
			//		Ordinal int    `json:"ordinal"`
			//	} `json:"question_sets"`
			//	SiteID     string `json:"site_id"`
			//	Googlejobs struct {
			//		CompanyName string `json:"companyName"`
			//		JobName     string `json:"jobName"`
			//		JobHash     string `json:"jobHash"`
			//		DerivedInfo struct {
			//			JobCategories []string `json:"jobCategories"`
			//			Locations     []struct {
			//				LatLng struct {
			//					Latitude  float64 `json:"latitude"`
			//					Longitude float64 `json:"longitude"`
			//				} `json:"latLng"`
			//				LocationType  string `json:"locationType"`
			//				PostalAddress struct {
			//					AddressLines       []string `json:"addressLines"`
			//					AdministrativeArea string   `json:"administrativeArea"`
			//					Locality           string   `json:"locality"`
			//					RegionCode         string   `json:"regionCode"`
			//				} `json:"postalAddress"`
			//				RadiusInMiles float64 `json:"radiusInMiles"`
			//			} `json:"locations"`
			//		} `json:"derivedInfo"`
			//		JobSummary string `json:"jobSummary"`
			//	} `json:"googlejobs"`
			//	CanonicalURL string `json:"canonical_url"`
			//	LastMod      string `json:"last_mod"`
			//	Gdpr         bool   `json:"gdpr"`
			//} `json:"meta_data"`
			//UpdateDate    string   `json:"update_date"`
			//CreateDate    string   `json:"create_date"`
			//Category      []string `json:"category"`
			FullLocation string `json:"full_location"`
			//ShortLocation string   `json:"short_location"`
		} `json:"data,omitempty"`
	} `json:"jobs,omitempty"`
	//MetaData struct {
	//	ResponseMetadata struct {
	//		RequestID string `json:"requestId"`
	//	} `json:"ResponseMetadata"`
	//} `json:"meta_data"`
	TotalCount int  `json:"totalCount"`
	Locations  bool `json:"locations"`
}

func getJibeJobsFor(ctx context.Context, company string) (<-chan *JobPosting, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s.jibeapply.com/api/jobs", company), nil)

	req = req.WithContext(ctx)

	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	// add limit
	q := req.URL.Query()
	q.Add("limit", "100")
	q.Add("page", "1")
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc := jibeJobs{}

	err = json.NewDecoder(resp.Body).Decode(&doc)
	if err != nil {
		return nil, err
	}

	nextBatch := func(doc *jibeJobs, page int) error {
		q := req.URL.Query()

		q.Set("page", fmt.Sprintf("%d", page))
		req.URL.RawQuery = q.Encode()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		err = json.NewDecoder(resp.Body).Decode(doc)

		if err != nil {
			return err
		}

		return nil
	}

	jobPostings := make(chan *JobPosting)

	go func() {
		defer close(jobPostings)

		for _, item := range doc.Jobs {
			url := strings.TrimSpace(strings.Replace(item.Data.ApplyURL, "http://", "https://", -1))
			titleStr := strings.TrimSpace(item.Data.Title)
			locationStr := strings.TrimSpace(item.Data.FullLocation)

			jobPostings <- &JobPosting{
				Company:  company,
				URL:      url,
				Title:    titleStr,
				Location: locationStr,
			}
		}

		found := len(doc.Jobs)

		for i := 1; found < doc.TotalCount; i++ {

			nextBatch(&doc, i)

			for _, item := range doc.Jobs {
				url := strings.TrimSpace(strings.Replace(item.Data.ApplyURL, "http://", "https://", -1))
				titleStr := strings.TrimSpace(item.Data.Title)
				locationStr := strings.TrimSpace(item.Data.FullLocation)

				jobPostings <- &JobPosting{
					Company:  company,
					URL:      url,
					Title:    titleStr,
					Location: locationStr,
				}
			}

			found += len(doc.Jobs)
		}

	}()

	return jobPostings, nil
}
