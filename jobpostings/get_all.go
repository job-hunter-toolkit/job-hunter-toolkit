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
		Get15FiveJobPostings,
		Get21stCenturyFoxJobPostings,
		Get3MJobPostings,
		GetAbacusJobPostings,
		GetAccionSystemsJobPostings,
		GetAdjustJobPostings,
		GetAdobeJobPostings,
		GetAESJobPostings,
		GetAffirmJobPostings,
		GetAfreshJobPostings,
		GetAirMapJobPostings,
		GetAirbnbJobPostings,
		GetAirtameJobPostings,
		GetAlterianJobPostings,
		GetAltoJobPostings,
		GetAmazeeJobPostings,
		GetAmenityAnalyticsJobPostings,
		GetAmyrisJobPostings,
		GetAnchorageJobPostings,
		GetApptioJobPostings,
		GetAptibleJobPostings,
		GetAquabyteJobPostings,
		GetArborJobPostings,
		GetArenaNetJobPostings,
		GetAstranisJobPostings,
		GetAtriumJobPostings,
		GetAuth0JobPostings,
		GetAware3JobPostings,
		GetAxonJobPostings,
		GetAzerionJobPostings,
		GetBackerKitJobPostings,
		GetBazaarVoiceJobPostings,
		GetBeamlyJobPostings,
		GetBetterLessonJobPostings,
		GetBetterUpJobPostings,
		GetBigHealthJobPostings,
		GetBigscreenJobPostings,
		GetBinanceJobPostings,
		GetBirdJobPostings,
		GetBishopFoxJobPostings,
		GetBitfishJobPostings,
		GetBitnamiJobPostings,
		GetBittrexJobPostings,
		GetBlackfynnJobPostings,
		GetBlamelessJobPostings,
		GetBlockstackJobPostings,
		GetBombasJobPostings,
		GetBoomSupersonicJobPostings,
		GetBoseJobPostings,
		GetBoxLunchJobPostings,
		GetBraintreeJobPostings,
		GetBrightBytesJobPostings,
		GetBrightwheelJobPostings,
		GetBritecoreJobPostings,
		GetBuildZoomJobPostings,
		GetBuildiumJobPostings,
		GetBustleJobPostings,
		GetBuzzFeedJobPostings,
		GetCAEJobPostings,
		GetCalmJobPostings,
		GetCamblyJobPostings,
		GetCanonicalJobPostings,
		GetCarbonBlackJobPostings,
		GetCarousellJobPostings,
		GetCartaJobPostings,
		GetCasperJobPostings,
		GetCatalyticJobPostings,
		GetCBInsightsJobPostings,
		GetChangeJobPostings,
		GetChartioJobPostings,
		GetCheddarJobPostings,
		GetChewyJobPostings,
		GetChimpJobPostings,
		GetCiscoMerakiJobPostings,
		GetCityAndCountyOfDenverJobPostings,
		GetClerkyJobPostings,
		GetClickTimeJobPostings,
		GetCloseJobPostings,
		GetCloudBeesJobPostings,
		GetCloudflareJobPostings,
		GetCloudreachJobPostings,
		GetCobaltRoboticsJobPostings,
		GetCockroachLabsJobPostings,
		GetCodacyJobPostings,
		GetCodeOceanJobPostings,
		GetCodecademyJobPostings,
		GetCoffeeMeetsBagelJobPostings,
		GetCoinbaseJobPostings,
		GetColossalJobPostings,
		GetCompassJobPostings,
		GetConductorTechnologiesJobPostings,
		GetCornellUniversityJobPostings,
		GetConsensysJobPostings,
		GetContrastSecurityJobPostings,
		GetConvoyJobPostings,
		GetCoreScientificJobPostings,
		GetCorelightJobPostings,
		GetCoupaJobPostings,
		GetCourseraJobPostings,
		GetCreditSesameJobPostings,
		GetCruiseJobPostings,
		GetCruxJobPostings,
		GetCuralateJobPostings,
		GetDahmakanJobPostings,
		GetDarkstoreJobPostings,
		GetDaticaJobPostings,
		GetDattoJobPostings,
		GetDaznJobPostings,
		GetDegreedJobPostings,
		GetDeliverooJobPostings,
		GetDellJobPostings,
		GetDigitalOceanJobPostings,
		GetDivvyHomesJobPostings,
		GetDnBJobPostings,
		GetDockerJobPostings,
		GetDominoJobPostings,
		GetDoorDashJobPostings,
		GetDotsJobPostings,
		GetDrChronoJobPostings,
		GetDropJobPostings,
		GetDuoLingoJobPostings,
		GetDuoSecurityJobPostings,
		GetEarlyWarningJobPostings,
		GetEburyJobPostings,
		GetEdenJobPostings,
		GetElasticJobPostings,
		GetEmbarkJobPostings,
		GetEpicGamesJobPostings,
		GetERMJobPostings,
		GetEssentialJobPostings,
		GetEventbriteJobPostings,
		GetEvercommerceJobPostings,
		GetEvernoteJobPostings,
		GetExpediaJobPostings,
		GetExpensifyJobPostings,
		GetFarmWiseJobPostings,
		GetFarmersBusinessNetworkJobPostings,
		GetFastlyJobPostings,
		GetFatLlamaJobPostings,
		GetFetchRevJobPostings,
		GetFICOJobPostings,
		GetFictivJobPostings,
		GetFingerFoodStudiosJobPostings,
		GetFirstJobPostings,
		GetFondJobPostings,
		GetForwardJobPostings,
		GetFossaJobPostings,
		GetFrontJobPostings,
		GetGameChangerJobPostings,
		GetGatesFoundationJobPostings,
		GetGeckoboardJobPostings,
		GetGenospaceJobPostings,
		GetGitHubJobPostings,
		GetGitLabJobPostings,
		GetGizmodoJobPostings,
		GetGlowJobPostings,
		GetGoDaddyJobPostings,
		GetGoatJobPostings,
		GetGojekJobPostings,
		GetGovPredictJobPostings,
		GetGradientJobPostings,
		GetGradleJobPostings,
		GetGrammarlyJobPostings,
		GetGreenpeaceJobPostings,
		GetGridspaceJobPostings,
		GetGrimmJobPostings,
		GetGuerrillaJobPostings,
		GetGustoJobPostings,
		GetHackerOneJobPostings,
		GetHashiCorpJobPostings,
		GetHazelAnalyticsJobPostings,
		GetHelloAlfredJobPostings,
		GetHelloFreshJobPostings,
		GetHelloSignJobPostings,
		GetHifyreJobPostings,
		GetHopsyJobPostings,
		GetImageworksJobPostings,
		GetImpossibleFoodsJobPostings,
		GetInboxLabJobPostings,
		GetInfluxDBJobPostings,
		GetInputJobPostings,
		GetInsightsoftwareJobPostings,
		GetInstabaseJobPostings,
		GetInstacartJobPostings,
		GetInstructureJobPostings,
		GetInstrumentalAIJobPostings,
		GetInventablesJobPostings,
		GetInvitaeJobPostings,
		GetIOTAJobPostings,
		GetIrisAutomationJobPostings,
		GetIronOxJobPostings,
		GetIroncladJobPostings,
		GetIssuuJobPostings,
		GetJDAJobPostings,
		GetJellyfishJobPostings,
		GetJetJobPostings,
		GetJoorJobPostings,
		GetJourneraJobPostings,
		GetJournyJobPostings,
		GetKantarJobPostings,
		GetKardJobPostings,
		GetKariusJobPostings,
		GetKayakJobPostings,
		GetKikJobPostings,
		GetKinnekJobPostings,
		GetKiteJobPostings,
		GetKlarnaJobPostings,
		GetKrakenJobPostings,
		GetLaunchDarklyJobPostings,
		GetLeverJobPostings,
		GetLightStepJobPostings,
		GetLighthouseStudiosJobPostings,
		GetLiltJobPostings,
		GetLimeJobPostings,
		GetLimeadeJobPostings,
		GetLinuxFoundationJobPostings,
		GetLiveNationJobPostings,
		GetLogDNAJobPostings,
		GetLogitechJobPostings,
		GetLookerJobPostings,
		GetLucidJobPostings,
		GetLyftJobPostings,
		GetLyricJobPostings,
		GetMagicLeapJobPostings,
		GetMajorLeagueBaseballPostings,
		GetMakeSchoolJobPostings,
		GetMakeSpaceJobPostings,
		GetMapboxJobPostings,
		GetMastercardJobPostings,
		GetMattermostJobPostings,
		GetMavenClinicJobPostings,
		GetMavensJobPostings,
		GetMeasurablJobPostings,
		GetMediumJobPostings,
		GetMessageBirdJobPostings,
		GetMetalToadJobPostings,
		GetMightyNetworksJobPostings,
		GetModernTimesBeerJobPostings,
		GetModernizeJobPostings,
		GetModsyJobPostings,
		GetMongoDBJobPostings,
		GetMovableInkJobPostings,
		GetMuxJobPostings,
		GetNarvarJobPostings,
		GetNashJobPostings,
		GetNationwideJobPostings,
		GetNetlifyJobPostings,
		GetNeuralinkJobPostings,
		GetNewEngenJobPostings,
		GetNewYorkTimesJobPostings,
		GetNewfrontInsuranceJobPostings,
		GetNexTravelobPostings,
		GetNexientJobPostings,
		GetNextdoorJobPostings,
		GetNovaJobPostings,
		GetNPMJobPostings,
		GetNS1JobPostings,
		GetNurxJobPostings,
		GetNvidiaJobPostings,
		GetNYMediaJobPostings,
		GetNylasJobPostings,
		GetOktaJobPostings,
		GetOmadaHealthJobPostings,
		GetOmazeJobPostings,
		GetOneLoginJobPostings,
		GetOpenAIJobPostings,
		GetOpenCosmosJobPostings,
		GetOpenFinJobPostings,
		GetOpenGovJobPostings,
		GetOpenMarketJobPostings,
		GetOpenPhoneJobPostings,
		GetOpendoorJobPostings,
		GetOptivJobPostings,
		GetOriginJobPostings,
		GetOutschoolJobPostings,
		GetPachydermJobPostings,
		GetPAEJobPostings,
		GetPagerDutyJobPostings,
		GetPalantirJobPostings,
		GetPaloAltoNetworksJobPostings,
		GetPaperspaceJobPostings,
		GetPathAIJobPostings,
		GetPathlightJobPostings,
		GetPatreonJobPostings,
		GetPAXJobPostings,
		GetPaytmJobPostings,
		GetPeopleAIJobPostings,
		GetPersistIQJobPostings,
		GetPetalJobPostings,
		GetPfizerJobPostings,
		GetPinterestJobPostings,
		GetPioneerSquareLabsJobPostings,
		GetPlacepassJobPostings,
		GetPlanGridJobPostings,
		GetPlatformshJobPostings,
		GetPolicygeniusJobPostings,
		GetPollEverywhereJobPostings,
		GetPopdogJobPostings,
		GetPostmatesJobPostings,
		GetPraetorianJobPostings,
		GetPredictiveIndexJobPostings,
		GetProcoreJobPostings,
		GetProjekt202JobPostings,
		GetProtocolJobPostings,
		GetPsiKickJobPostings,
		GetPushpayJobPostings,
		GetQualtricsJobPostings,
		GetQualysJobPostings,
		GetQuartzyJobPostings,
		GetQuoraJobPostings,
		GetRainforestQAJobPostings,
		GetRapid7JobPostings,
		GetRecurlyJobPostings,
		GetRedditJobPostings,
		GetReifyHealthJobPostings,
		GetRelativityJobPostings,
		GetRelayrJobPostings,
		GetRemarkablyJobPostings,
		GetRemitlyJobPostings,
		GetRemixJobPostings,
		GetReplateJobPostings,
		GetResearchGateJobPostings,
		GetReturntocorpJobPostings,
		GetRiotGamesJobPostings,
		GetRobinhoodJobPostings,
		GetRobloxJobPostings,
		GetRollPayJobPostings,
		GetRollsRoyceJobPostings,
		GetRoosterTeethJobPostings,
		GetRoverJobPostings,
		GetRubrikJobPostings,
		GetSalesforceJobPostings,
		GetSaltStackJobPostings,
		GetSamsaraJobPostings,
		GetSamsungJobPostings,
		GetSauceLabsJobPostings,
		GetScaleAIJobPostings,
		GetScanditJobPostings,
		GetScoopJobPostings,
		GetScreenCloudJobPostings,
		GetScribdJobPostings,
		GetSecondMeasureJobPostings,
		GetSecondSpectrumJobPostings,
		GetSecurityTrailsJobPostings,
		GetSecuronixJobPostings,
		GetSemaphoreSolutionsJobPostings,
		GetSensorTowerJobPostings,
		GetSentryJobPostings,
		GetShapeScaleCraterJobPostings,
		GetShoesJobPostings,
		GetShopKeepJobPostings,
		GetShopifyJobPostings,
		GetSiftJobPostings,
		GetSignalJobPostings,
		GetSimpliSafeJobPostings,
		GetSkillshareJobPostings,
		GetSkipJobPostings,
		GetSkydioJobPostings,
		GetSlackJobPostings,
		GetSmarkingJobPostings,
		GetSmartsheetJobPostings,
		GetSnapJobPostings,
		GetSnapRaiseJobPostings,
		GetSnapTravelJobPostings,
		GetSnapdocsJobPostings,
		GetSnowflakeJobPostings,
		GetSonyPlayStationJobPostings,
		GetSourceDJobPostings,
		GetSourceressJobPostings,
		GetSpaceflightIndustriesJobPostings,
		GetSpaceXJobPostings,
		GetSparkswapJobPostings,
		GetSpotHeroJobPostings,
		GetSpotifyJobPostings,
		GetSproutSocialobPostings,
		GetSquarespaceJobPostings,
		GetStackAdaptJobPostings,
		GetStarcityJobPostings,
		GetStarshipJobPostings,
		GetStarskyRoboticsJobPostings,
		GetStateStreetJobPostings,
		GetStauerJobPostings,
		GetStravaJobPostings,
		GetStreamlabsJobPostings,
		GetStripeJobPostings,
		GetSumoLogicJobPostings,
		GetSurveyGizmoJobPostings,
		GetSurveyMonkeyJobPostings,
		GetSwatJobPostings,
		GetSwissBorgJobPostings,
		GetSymantecJobPostings,
		GetSysdigJobPostings,
		GetT1CGJobPostings,
		GetTableauJobPostings,
		GetTablexiJobPostings,
		GetTacoBellJobPostings,
		GetTalaJobPostings,
		GetTaplyticsJobPostings,
		GetTeamableJobPostings,
		GetTelariaJobPostings,
		GetTheAthleticJobPostings,
		GetTheTradeDeskJobPostings,
		GetThunderTokenJobPostings,
		GetTiltingPointJobPostings,
		GetTimeJobPostings,
		GetTimeIncJobPostings,
		GetTopHatJobPostings,
		GetToptalJobPostings,
		GetTransLifelineJobPostings,
		GetTranscendJobPostings,
		GetTransparentSystemsJobPostings,
		GetTrilogyEducationJobPostings,
		GetTripAdvisorJobPostings,
		GetTripleByteJobPostings,
		GetTTTStudiosJobPostings,
		GetTuftAndNeedleJobPostings,
		GetTuneJobPostings,
		GetTutelaJobPostings,
		GetTwilioJobPostings,
		GetTwitchJobPostings,
		GetTyroJobPostings,
		GetUberJobPostings,
		GetUberEtherJobPostings,
		GetUdacityJobPostings,
		GetUniregistryJobPostings,
		GetUnisysJobPostings,
		GetUnity3DJobPostings,
		GetUpstartJobPostings,
		GetUpworkJobPostings,
		GetUserLeapJobPostings,
		GetVenafiJobPostings,
		GetVendJobPostings,
		GetVenmoJobPostings,
		GetVergeGenomicsJobPostings,
		GetVerifoneJobPostings,
		GetVerishopJobPostings,
		GetVeritasJobPostings,
		GetVerizonMediaJobPostings,
		GetVewdJobPostings,
		GetViceJobPostings,
		GetVoleonJobPostings,
		GetVoodooJobPostings,
		GetVoxMediaJobPostings,
		GetVoxterJobPostings,
		GetWealthsimpleJobPostings,
		GetWeb3JobPostings,
		GetWellframeJobPostings,
		GetWGamesJobPostings,
		GetWheelsJobPostings,
		GetWhisperJobPostings,
		GetWholeFoodsJobPostings,
		GetWikimediaJobPostings,
		GetWizelineJobPostings,
		GetWonderJobPostings,
		GetXylemJobPostings,
		GetZendeskJobPostings,
		GetZenefitsJobPostings,
		GetZentailJobPostings,
		GetZeplinJobPostings,
		GetZeroCraterJobPostings,
		GetZeroFoxJobPostings,
		GetZeusJobPostings,
		GetZiplineJobPostings,
		GetZipwhipJobPostings,
		GetZumeJobPostings,
		GetZyrisJobPostings,
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
