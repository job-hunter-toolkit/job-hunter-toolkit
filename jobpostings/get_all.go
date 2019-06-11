package jobpostings

import (
	"context"
	"sync"
)

// GetAllJobPostings finds all of the JobPostings using every source.
func GetAllJobPostings(ctx context.Context) <-chan *JobPosting {
	// cat *.go | grep "\bGet" | grep "ctx" | awk '{print $2}' | awk -F '(' '{print $1","}' | grep "Get" | grep -v "GetAllJob" | pbcopy
	all := []func(ctx context.Context) (<-chan *JobPosting, error){
		Get21stCenturyFoxJobPostings,
		Get3MJobPostings,
		GetAdobeJobPostings,
		GetAESJobPostings,
		GetAirbnbJobPostings,
		GetBoseJobPostings,
		GetBuzzFeedJobPostings,
		GetCAEJobPostings,
		GetCarbonBlackJobPostings,
		GetCasperJobPostings,
		GetCiscoMerakiJobPostings,
		GetCityAndCountyOfDenverJobPostings,
		GetCloudreachJobPostings,
		GetCoinbaseJobPostings,
		GetCornellUniversityJobPostings,
		GetDattoJobPostings,
		GetDellJobPostings,
		GetDnBJobPostings,
		GetDuoSecurityJobPostings,
		GetElasticJobPostings,
		GetEpicGamesJobPostings,
		GetERMJobPostings,
		GetEvernoteJobPostings,
		GetExpediaJobPostings,
		GetFICOJobPostings,
		GetGatesFoundationJobPostings,
		GetGitHubJobPostings,
		GetGitLabJobPostings,
		GetHashiCorpJobPostings,
		GetHelloFreshJobPostings,
		GetImageworksJobPostings,
		GetInstacartJobPostings,
		GetJDAJobPostings,
		GetKantarJobPostings,
		GetLiveNationJobPostings,
		GetLyftJobPostings,
		GetMajorLeagueBaseballPostings,
		GetMastercardJobPostings,
		GetNationwideJobPostings,
		GetNetlifyJobPostings,
		GetNewYorkTimesJobPostings,
		GetNvidiaJobPostings,
		GetOktaJobPostings,
		GetPAEJobPostings,
		GetPfizerJobPostings,
		GetPinterestJobPostings,
		GetRapid7JobPostings,
		GetRedditJobPostings,
		GetRiotGamesJobPostings,
		GetRollsRoyceJobPostings,
		GetSalesforceJobPostings,
		GetSamsungJobPostings,
		GetSquarespaceJobPostings,
		GetStateStreetJobPostings,
		GetStripeJobPostings,
		GetSumoLogicJobPostings,
		GetSurveyMonkeyJobPostings,
		GetSymantecJobPostings,
		GetTacoBellJobPostings,
		GetToptalJobPostings,
		GetTripAdvisorJobPostings,
		GetTwilioJobPostings,
		GetUberJobPostings,
		GetUberEtherJobPostings,
		GetUnisysJobPostings,
		GetVenmoJobPostings,
		GetVeritasJobPostings,
		GetVerizonMediaJobPostings,
		GetVoxMediaJobPostings,
		GetWholeFoodsJobPostings,
		GetZenefitsJobPostings,
	}

	allJobPostings := make(chan *JobPosting)

	go func() {
		defer close(allJobPostings)

		wg := sync.WaitGroup{}

		for _, jobPostingSource := range all {
			jobPostingChannel, err := jobPostingSource(ctx)

			if err == nil {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for singlePosting := range jobPostingChannel {
						select {
						case allJobPostings <- singlePosting:
						case <-ctx.Done():
							return
						}
					}
				}()
			}
		}

		wg.Wait()
	}()

	return allJobPostings
}
