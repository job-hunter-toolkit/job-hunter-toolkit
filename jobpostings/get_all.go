package jobpostings

import (
	"context"
	"sync"
)

// GetAllJobPostings finds all of the JobPostings using every source.
func GetAllJobPostings(ctx context.Context) <-chan *JobPosting {
	// cat *.go | grep "\bGet" | grep "ctx" | awk '{print $2}' | awk -F '(' '{print $1","}' | grep "Get" | grep -v "GetAllJob" | pbcopy
	all := []func(ctx context.Context) (<-chan *JobPosting, error){
		Get0xJobPostings,
		Get21stCenturyFoxJobPostings,
		Get3MJobPostings,
		GetAdjustJobPostings,
		GetAdobeJobPostings,
		GetAESJobPostings,
		GetAirMapJobPostings,
		GetAirbnbJobPostings,
		GetAirtameJobPostings,
		GetAltoJobPostings,
		GetAnchorageJobPostings,
		GetBeamlyJobPostings,
		GetBetterUpJobPostings,
		GetBittrexJobPostings,
		GetBombasJobPostings,
		GetBoseJobPostings,
		GetBraintreeJobPostings,
		GetBuildiumJobPostings,
		GetBuzzFeedJobPostings,
		GetCAEJobPostings,
		GetCanonicalJobPostings,
		GetCarbonBlackJobPostings,
		GetCarousellJobPostings,
		GetCasperJobPostings,
		GetCBInsightsJobPostings,
		GetCheddarJobPostings,
		GetCiscoMerakiJobPostings,
		GetCityAndCountyOfDenverJobPostings,
		GetCloudflareJobPostings,
		GetCloudreachJobPostings,
		GetCoinbaseJobPostings,
		GetCornellUniversityJobPostings,
		GetCoreScientificJobPostings,
		GetCuralateJobPostings,
		GetDattoJobPostings,
		GetDellJobPostings,
		GetDigitalOceanJobPostings,
		GetDnBJobPostings,
		GetDotsJobPostings,
		GetDropJobPostings,
		GetDuoSecurityJobPostings,
		GetEarlyWarningJobPostings,
		GetElasticJobPostings,
		GetEpicGamesJobPostings,
		GetERMJobPostings,
		GetEvernoteJobPostings,
		GetExpediaJobPostings,
		GetExpensifyJobPostings,
		GetFarmersBusinessNetworkJobPostings,
		GetFastlyJobPostings,
		GetFICOJobPostings,
		GetFirstJobPostings,
		GetGatesFoundationJobPostings,
		GetGitHubJobPostings,
		GetGitLabJobPostings,
		GetGoDaddyJobPostings,
		GetGridspaceJobPostings,
		GetHashiCorpJobPostings,
		GetHelloFreshJobPostings,
		GetImageworksJobPostings,
		GetInputJobPostings,
		GetInstacartJobPostings,
		GetInventablesJobPostings,
		GetInvitaeJobPostings,
		GetJDAJobPostings,
		GetKantarJobPostings,
		GetKayakJobPostings,
		GetKinnekJobPostings,
		GetLightStepJobPostings,
		GetLiveNationJobPostings,
		GetLogitechJobPostings,
		GetLyftJobPostings,
		GetLyricJobPostings,
		GetMagicLeapJobPostings,
		GetMajorLeagueBaseballPostings,
		GetMastercardJobPostings,
		GetMavenClinicJobPostings,
		GetNarvarJobPostings,
		GetNationwideJobPostings,
		GetNetlifyJobPostings,
		GetNewYorkTimesJobPostings,
		GetNexientJobPostings,
		GetNvidiaJobPostings,
		GetOktaJobPostings,
		GetOmadaHealthJobPostings,
		GetOmazeJobPostings,
		GetPAEJobPostings,
		GetPaloAltoNetworksJobPostings,
		GetPathlightJobPostings,
		GetPatreonJobPostings,
		GetPAXJobPostings,
		GetPfizerJobPostings,
		GetPinterestJobPostings,
		GetPolicygeniusJobPostings,
		GetPredictiveIndexJobPostings,
		GetPsiKickJobPostings,
		GetQualysJobPostings,
		GetRapid7JobPostings,
		GetRedditJobPostings,
		GetRiotGamesJobPostings,
		GetRobinhoodJobPostings,
		GetRollsRoyceJobPostings,
		GetSalesforceJobPostings,
		GetSamsaraJobPostings,
		GetSamsungJobPostings,
		GetScanditJobPostings,
		GetSentryJobPostings,
		GetShoesJobPostings,
		GetSnapRaiseJobPostings,
		GetSpaceXJobPostings,
		GetSpotifyJobPostings,
		GetSquarespaceJobPostings,
		GetStateStreetJobPostings,
		GetStauerJobPostings,
		GetStravaJobPostings,
		GetStripeJobPostings,
		GetSumoLogicJobPostings,
		GetSurveyMonkeyJobPostings,
		GetSymantecJobPostings,
		GetTableauJobPostings,
		GetTacoBellJobPostings,
		GetTelariaJobPostings,
		GetTimeIncJobPostings,
		GetToptalJobPostings,
		GetTripAdvisorJobPostings,
		GetTwilioJobPostings,
		GetUberJobPostings,
		GetUberEtherJobPostings,
		GetUdacityJobPostings,
		GetUnisysJobPostings,
		GetVenafiJobPostings,
		GetVenmoJobPostings,
		GetVerifoneJobPostings,
		GetVerishopJobPostings,
		GetVeritasJobPostings,
		GetVerizonMediaJobPostings,
		GetViceJobPostings,
		GetVoxMediaJobPostings,
		GetWhisperJobPostings,
		GetWholeFoodsJobPostings,
		GetXylemJobPostings,
		GetZenefitsJobPostings,
		GetZumeJobPostings,
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
