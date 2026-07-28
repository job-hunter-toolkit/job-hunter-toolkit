package services

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// successFactorsPlatform is the ATS family this file registers, and the value
// that reaches [internal.PostingSource.Platform].
const successFactorsPlatform = "successfactors"

func init() {
	registerBuiltin(successFactorsPlatform, multiJobsFuncNamed(SuccessFactors, SuccessFactorsTenants, successFactorsCompanyName))
}

// successFactorsMaxResponseBytes bounds one tenant's feed.
//
// This adapter does not paginate: SAP's "Recruiting Marketing" (RMK) feed
// answers a single GET with an enterprise's entire open-req corpus and full HTML
// descriptions inline. That is what makes it the cheapest enterprise lane in the
// project by requests, and it is also why there is no page loop here to put a
// ceiling on.
//
// The equivalent bound for a single-shot adapter is on the response instead. The
// whole body has to be held in memory to be scanned (see [successFactorsJob]),
// so a tenant that answers with a stream that never ends, or with something that
// is not this feed at all, would otherwise be free to consume a worker's memory
// for as long as the crawl runs.
//
// The number is measured rather than assumed, and the measurement corrected it.
// Probing all 739 live tenants on 2026-07-28 put the mean feed at 1.34 MB and
// the largest, CRH, at 27.1 MB — which is 81% of the 32 MiB this constant used
// to be, not the "roughly twenty times the largest tenant" its comment claimed.
// A quarter of a year's growth at CRH would have started failing the source with
// a size error. 64 MiB is 2.4x the largest feed anyone has seen, and hitting it
// is still reported as an error rather than parsed as a short feed: a truncated
// document would otherwise silently drop the tail of an employer's postings.
const successFactorsMaxResponseBytes = 64 << 20

// SuccessFactorsTenants holds the SAP SuccessFactors RMK career sites this
// project crawls, one "slug,companyId,host" triple per entry.
//
// Tenancy here is a triple, not a slug, and none of the three parts is
// guessable. companyId is the ?company= value the career site was configured
// with and is case-sensitive ("nestleHRprdBX", "C0000159936P"); host is
// career{N}.successfactors.{com|eu}, where both the number and the TLD are
// tenant-specific — career5.successfactors.com is NXDOMAIN while
// career5.successfactors.eu is live. The slug is this project's own name for the
// employer and is what a person types after --company.
//
// # Where this list came from
//
// All 744 candidate triples were probed live on 2026-07-28, and each entry below
// answered with a <Job-Listing> root and a non-zero <Job> count:
//
//	739 answered with postings   3 answered empty   2 failed to connect
//
// carrying 106,307 postings between them. One request per employer, no
// pagination, apply URLs synthesized — that is ~144 postings per HTTP request,
// by a wide margin the best ratio of any platform here, and the reason this lane
// matters to a crawl docs/architecture-roadmap.md records as unable to finish.
// The cost is bytes rather than requests: the 739 feeds total 0.99 GB, averaging
// 1.34 MB and peaking at 27.1 MB for CRH. See [successFactorsMaxResponseBytes].
//
// 717 of the 739 are registered here. Held back, with the reason:
//
//   - 16 tenants served from career{N}.sapsf.com and career-hcm20.ns2cloud.com
//     (5,685 postings, including L3Harris, GXO, Seagate and Kodak). Those hosts
//     are real and answer, but they match no arm of httpx.servicePolicyFor, so
//     their requests would be unpaced — the exact omission
//     TestEveryPlatformHasAPacingPolicy exists to catch. They are ready to
//     promote the moment httpx learns those two suffixes.
//   - 5 whose slug is already a registered company on another platform
//     (bechtel, cornell, farmersinsurance, msd, pwc). [internal.Dedupe] keys on
//     URL, so the same employer crawled through two ATSs contributes its
//     postings twice; TestSuccessFactorsAddsNoDoubleCountedEmployer is the
//     guard, and one route has to be chosen deliberately rather than here.
//   - 1 whose slug is a bare number ("160100", Southco). The slug is the
//     user-facing company name and must stay verbatim from the candidate file,
//     so this one needs the candidate file corrected before it can be promoted.
var SuccessFactorsTenants = []string{
	"aaw,aliabdulwa,career2.successfactors.eu",
	"abports,C0014373926P,career5.successfactors.eu",
	"action,Action,career5.successfactors.eu",
	"adbsafegate,ADBDP,career5.successfactors.eu",
	"adidas,AdidasP,career5.successfactors.eu",
	"advancedenergy,advanceden,career4.successfactors.com",
	"afp,digitaltra,career10.successfactors.com",
	"agcocorp,Agco,career4.successfactors.com",
	"agfa,agfagevaer,career2.successfactors.eu",
	"agrifirm,C0000033858P,career5.successfactors.eu",
	"agthia,agthiagrouP,career2.successfactors.eu",
	"aib,aib,career2.successfactors.eu",
	"ajinomotobe,ajinomotoc,career2.successfactors.eu",
	"akzonobel,akzonobelsP2,career5.successfactors.eu",
	"aldihofer,HoferSELive,career5.successfactors.eu",
	"alfanar,AlFanarP,career5.successfactors.eu",
	"alfasigma,alfasigmas,career5.successfactors.eu",
	"allforone,all41Group,career2.successfactors.eu",
	"allnex,allnexbelg,career2.successfactors.eu",
	"alpiq,Alpiq,career5.successfactors.eu",
	"altana,altanamana,career5.successfactors.eu",
	"ambu,ambu,career5.successfactors.eu",
	"amex,americairP,career4.successfactors.com",
	"amtconsulting,amt,career2.successfactors.eu",
	"amway,Amway,career4.successfactors.com",
	"andritz,andritzag,career2.successfactors.eu",
	"anglogoldashanti,AGAprod,career5.successfactors.eu",
	"anz,anzbanking,career10.successfactors.com",
	"aofoundation,AOF,career5.successfactors.eu",
	"aosmith,aosmith,career8.successfactors.com",
	"ap,associated,career8.successfactors.com",
	"apachecorp,apachecorp,career4.successfactors.com",
	"aptar,aptaritali,career2.successfactors.eu",
	"aramex,AramexP,career5.successfactors.eu",
	"arisglobal,arisglobalP2,career10.successfactors.com",
	"arjo,arjohuntle,career5.successfactors.eu",
	"arlanxeo,arlanxeode,career5.successfactors.eu",
	"assaabloy,assaabloya,career2.successfactors.eu",
	"atlanticgrupa,atlanticgr,career2.successfactors.eu",
	"atos,Atos,career5.successfactors.eu",
	"ats,C0001087355P,career5.successfactors.eu",
	"atsautomation,atsP2,career5.successfactors.eu",
	"aucklandcouncil,aucklandcoP,career10.successfactors.com",
	"ausgrid,ausgrid,career10.successfactors.com",
	"ausnetservices,ausnetelecP,career10.successfactors.com",
	"aviation,civilaviat,career10.successfactors.com",
	"avl,avllistgmb,career2.successfactors.eu",
	"avoltaworld,avoltaP,career5.successfactors.eu",
	"azgroup,AZGROUPPROD,career5.successfactors.eu",
	"babcockinternational,C0020126214P,career5.successfactors.eu",
	"bajajauto,BAL,career10.successfactors.com",
	"ball,ballcorpor,career4.successfactors.com",
	"bank,providentbank,career4.successfactors.com",
	"bankislam,bankislammP2,career10.successfactors.com",
	"bankwithunited,unitedbank,career8.successfactors.com",
	"barillagroup,barilla,career2.successfactors.eu",
	"barrycallebaut,BARCALPRD,career5.successfactors.eu",
	"bartonmalow,BartonMalowP,career4.successfactors.com",
	"basf,C0000159936P,career5.successfactors.eu",
	"bauhaus,bahagag,career5.successfactors.eu",
	"bayer,C0003153479P,career5.successfactors.eu",
	"bbraun,bbraunprd,career5.successfactors.eu",
	"bce,bcci,career8.successfactors.com",
	"bce2,Bell,career5.successfactors.eu",
	"bcm,BCM,career4.successfactors.com",
	"bcv,BCV,career5.successfactors.eu",
	"bcx,BCXP,career5.successfactors.eu",
	"beachenergy,C0015617690P,career10.successfactors.com",
	"beelinegroup,beeline,career5.successfactors.eu",
	"belagricola,C0004831526P,career4.successfactors.com",
	"belden,beldeninc,career4.successfactors.com",
	"belgiantrain,nmbsnation,career2.successfactors.eu",
	"benteler,BENTELER,career5.successfactors.eu",
	"bertelsmann,Bertelsmann,career5.successfactors.eu",
	"bertrandt,bertrandta,career5.successfactors.eu",
	"bestseller,bestseller,career2.successfactors.eu",
	"betagro,betagropub,career10.successfactors.com",
	"bethel,vbodelschw,career5.successfactors.eu",
	"bgbau,bgbau,career5.successfactors.eu",
	"bgphoenics,bgphoenicsP2,career5.successfactors.eu",
	"bgv,bgvversich,career5.successfactors.eu",
	"bhtc,bhtcbehrhe,career5.successfactors.eu",
	"bigdutchman,bigdutchma,career5.successfactors.eu",
	"bilfinger,bilfingers,career5.successfactors.eu",
	"bingoindustries,synchronysP9,career10.successfactors.com",
	"biontech,biontechse,career5.successfactors.eu",
	"bluepharmagroup,bluepharmaP,career5.successfactors.eu",
	"bmt,bmtinterna,career2.successfactors.eu",
	"bmw,bmwag,career5.successfactors.eu",
	"boehringer,BoehringerPRD,career5.successfactors.eu",
	"bokf,BOKF,career4.successfactors.com",
	"boltongroup,boltongrou,career2.successfactors.eu",
	"bombardier,Bombardier,career5.successfactors.eu",
	"bonduelle,bonduelleP,career5.successfactors.eu",
	"bonfiglioli,C0001104584P,career5.successfactors.eu",
	"borealisgroup,borealisag,career2.successfactors.eu",
	"bourns,bournsinc,career4.successfactors.com",
	"bouyguesconstruction,bouyguescoP,career5.successfactors.eu",
	"brandtholdings,brandthold,career4.successfactors.com",
	"breitling,breitlinga,career5.successfactors.eu",
	"bridgestoneasiapacific,bridgest01,career10.successfactors.com",
	"brighthousefinancial,brighthous,career8.successfactors.com",
	"britvic,Britvic,career5.successfactors.eu",
	"brose,brosefahrz,career2.successfactors.eu",
	"browardschools,browardcou,career8.successfactors.com",
	"brunatametrona,brunatawrm,career5.successfactors.eu",
	"bt,britisht01P1,career2.successfactors.eu",
	"bud,budapestaiP2,career2.successfactors.eu",
	"bunge,Bunge,career5.successfactors.eu",
	"burberry,Burberry,career5.successfactors.eu",
	"bureauveritas,BVPROD,career5.successfactors.eu",
	"bv,blackveatch2,career4.successfactors.com",
	"bwxt,C0011463572P,career4.successfactors.com",
	"c0002497004p,C0002497004P,career10.successfactors.com",
	"caa,CAA,career2.successfactors.eu",
	"cabbchemicals,CABB,career5.successfactors.eu",
	"caixabank,CaixaBank,career2.successfactors.eu",
	"calik,calikholdi,career2.successfactors.eu",
	"camparigroup,davideca01P,career5.successfactors.eu",
	"canadalife,thegreatweP2,career5.successfactors.eu",
	"canalplus,groupecana,career2.successfactors.eu",
	"canon,octechnoloP,career5.successfactors.eu",
	"capgemini,capgemitecP3,career5.successfactors.eu",
	"capitecbank,capitecban,career2.successfactors.eu",
	"careersatmediq,Mediq,career5.successfactors.eu",
	"carestream,carestream,career4.successfactors.com",
	"cargill,cargill,career2.successfactors.eu",
	"carrerasenred,redelectri,career5.successfactors.eu",
	"casais,casaisenge,career2.successfactors.eu",
	"ccbss,CCUSP1,career4.successfactors.com",
	"cdmig,cdmservice,career4.successfactors.com",
	"celestica,celesticaiP1,career2.successfactors.eu",
	"cemex,emexcentrP2,career5.successfactors.eu",
	"centerpointenergy,centerpoin,career4.successfactors.com",
	"centralbedfordshire,centralbed,career2.successfactors.eu",
	"cewa,catholiced,career10.successfactors.com",
	"cflex,CFlex,career5.successfactors.eu",
	"chainiq,chainiqgro,career5.successfactors.eu",
	"chartindustries,ChartUrCourse,career4.successfactors.com",
	"checkers,checkersdr,career8.successfactors.com",
	"chinachemgroup,chinachema,career10.successfactors.com",
	"cityedgedevelopments,cityedgede,career2.successfactors.eu",
	"clariant,Clariant,career2.successfactors.eu",
	"cmacgmgroup,C0002716868P,career2.successfactors.eu",
	"cmc,C0015784444P,career4.successfactors.com",
	"coats,jpcoatsltd,career5.successfactors.eu",
	"codan,C0002469119P,career10.successfactors.com",
	"colgate,colgate,career4.successfactors.com",
	"coloplast,Coloplast,career5.successfactors.eu",
	"commscope,CommScope,career4.successfactors.com",
	"computacenter,C0000167518P,career5.successfactors.eu",
	"condis,condissupe,career2.successfactors.eu",
	"congatec,congatecag,career2.successfactors.eu",
	"conocophillips,CPC,career4.successfactors.com",
	"constellium,C0007086225P,career5.successfactors.eu",
	"constructionbenefits,S001958294P,career4.successfactors.com",
	"consumersenergy,CMS,career4.successfactors.com",
	"coop,Coop,career2.successfactors.eu",
	"corbion,corbion,career2.successfactors.eu",
	"corning,CNGPROD,career4.successfactors.com",
	"cosentino,C0003558050P,career5.successfactors.eu",
	"coty,Coty,career2.successfactors.eu",
	"creamosdesarrollooportunidadeslaborales,ferreyrossP,career4.successfactors.com",
	"crh,CRH,career2.successfactors.eu",
	"crocs,crocs,career4.successfactors.com",
	"crown,C0000169989P,career4.successfactors.com",
	"crystalgroup,crystalkni,career10.successfactors.com",
	"csiro,CSIRO,career10.successfactors.com",
	"cybexonline,C0015267129P,career5.successfactors.eu",
	"daicompanies,dataanalys,career4.successfactors.com",
	"daikin,daikineuro,career2.successfactors.eu",
	"dana,DanaLimitedP,career4.successfactors.com",
	"danfoss,danfossas,career2.successfactors.eu",
	"danishcrown,danishcrow,career5.successfactors.eu",
	"dart,C0000176760P,career4.successfactors.com",
	"datwyler,dtwylerits,career2.successfactors.eu",
	"davey,DaveyTree,career8.successfactors.com",
	"dawnfoods,dfprod,career4.successfactors.com",
	"dcareer,lisadrxlma,career5.successfactors.eu",
	"dcc,C0001243376P,career5.successfactors.eu",
	"decathlon,gavdiasP9,career5.successfactors.eu",
	"delekus,Delek,career4.successfactors.com",
	"delicato,delicatoviP,career4.successfactors.com",
	"deloitte,CADeloitte,career8.successfactors.com",
	"deloitte2,DeloitteAu,career10.successfactors.com",
	"deloitte3,C0021622709P,career5.successfactors.eu",
	"denora,industried,career2.successfactors.eu",
	"dentons,dentonsgro,career2.successfactors.eu",
	"dentsplysirona,DENTSPLY,career5.successfactors.eu",
	"desigual,Desigual,career5.successfactors.eu",
	"deutscheboerse,Dboerse,career5.successfactors.eu",
	"dfinsolutions,donnelleyf,career8.successfactors.com",
	"discovery,discoveryhP,career2.successfactors.eu",
	"dlr,dlrdeutsch,career5.successfactors.eu",
	"doehler,dhlergmbh,career5.successfactors.eu",
	"dolby,Dolby,career4.successfactors.com",
	"dolcegabbana,dolcegabba,career2.successfactors.eu",
	"dormakaba,C0003136120P,career5.successfactors.eu",
	"dormanproducts,S000008602P,career4.successfactors.com",
	"dovercorporation,DOVER,career2.successfactors.eu",
	"draeger,draegerP,career5.successfactors.eu",
	"drvmd,SFP09,career5.successfactors.eu",
	"dsv,dsvas,career2.successfactors.eu",
	"dteenergy,DTEENERGYCO,career4.successfactors.com",
	"dtswiss,DTSwiss,career5.successfactors.eu",
	"dufry,C0004598545P,career5.successfactors.eu",
	"duke,dukeuniverP1,career4.successfactors.com",
	"dwrcymru,dwrcymrucy,career2.successfactors.eu",
	"dzprivatbank,dzbankagP3,career5.successfactors.eu",
	"eastman,eastmanprd,career4.successfactors.com",
	"ebrd,C0009154570P,career5.successfactors.eu",
	"ebscoind,ebscoindus,career8.successfactors.com",
	"ebzgroup,ebzse,career2.successfactors.eu",
	"ecco,eccoskoas,career2.successfactors.eu",
	"ecotone,RW,career2.successfactors.eu",
	"edfenergy,EDFEDP3,career5.successfactors.eu",
	"edgewell,EPCPROD,career8.successfactors.com",
	"edp,EDP,career5.successfactors.eu",
	"eg,eurogarage,career2.successfactors.eu",
	"eisai,EisaiP,career5.successfactors.eu",
	"eliagroup,eliasystem,career2.successfactors.eu",
	"emch,emchaufzge,career5.successfactors.eu",
	"emergentbiosolutions,EBSI,career8.successfactors.com",
	"enableinjections,enableinje,career4.successfactors.com",
	"encevo,Enovos,career5.successfactors.eu",
	"endress,endress,career5.successfactors.eu",
	"eon,EonProd,career5.successfactors.eu",
	"epa,environmen,career10.successfactors.com",
	"eramet,ERAMET,career5.successfactors.eu",
	"ericsson,Ericsson,career2.successfactors.eu",
	"erzbistummuenchen,erzdizesem,career5.successfactors.eu",
	"esa,esa,career2.successfactors.eu",
	"esb,electric02,career2.successfactors.eu",
	"essaroil,essaroiluk,career2.successfactors.eu",
	"esteiermark,energieste,career2.successfactors.eu",
	"esteve,C0001102163P,career5.successfactors.eu",
	"europa,europeanmeP,career5.successfactors.eu",
	"eutelsat,2236044P,career2.successfactors.eu",
	"evershedssutherland,evershedss,career2.successfactors.eu",
	"evertecinc,evertecgro,career8.successfactors.com",
	"evolutionmining,evolutionm,career10.successfactors.com",
	"excelitas,excelitast,career5.successfactors.eu",
	"exxonmobil,exxonmobilP,career4.successfactors.com",
	"farmcrediteast,farmcredit,career4.successfactors.com",
	"farys,tmvw,career2.successfactors.eu",
	"fbdgroup,fbdholding,career2.successfactors.eu",
	"fccasrgroup,634633P,career4.successfactors.com",
	"fcx,freeportmc,career4.successfactors.com",
	"fernunihagen,fernuniver,career2.successfactors.eu",
	"festo,festoagcokP,career5.successfactors.eu",
	"fft,fftprodukt,career5.successfactors.eu",
	"firstcitizenstt,firstcit01,career5.successfactors.eu",
	"fitch,C0016306184P,career5.successfactors.eu",
	"flowersfoods,FlocorpPRD,career4.successfactors.com",
	"foodstuffssi,foodstuffs,career10.successfactors.com",
	"foodtravelexperts,sspfinanci,career5.successfactors.eu",
	"fortisalberta,C0000208235P,career4.successfactors.com",
	"fphcare,fphcPRD,career10.successfactors.com",
	"fr,administ04,career2.successfactors.eu",
	"franke,FrankeP,career5.successfactors.eu",
	"frasersproperty,tcctechnolP1,career5.successfactors.eu",
	"fraunhofer,fraunhofer,career5.successfactors.eu",
	"frostaag,frostaag,career5.successfactors.eu",
	"fuchs,FUCHSTP2,career5.successfactors.eu",
	"g3enterprises,G3,career4.successfactors.com",
	"gallo,Gallo,career4.successfactors.com",
	"garda,garda,career4.successfactors.com",
	"gatewayfoundation,gatewayfou,career4.successfactors.com",
	"gebrheinemann,heinemannP1,career5.successfactors.eu",
	"gedeonrichter,richterged,career2.successfactors.eu",
	"gent,4079913P,career5.successfactors.eu",
	"gentera,C0005807193P,career8.successfactors.com",
	"gestamp,gestampser,career2.successfactors.eu",
	"getinge,GetingeProd,career5.successfactors.eu",
	"gfgalliance,C0001117189P,career10.successfactors.com",
	"gft,gfttechnol,career5.successfactors.eu",
	"gibsonenergy,gibsons,career4.successfactors.com",
	"givaudan,givaudan,career5.successfactors.eu",
	"gknaerospace,gknaerospa,career2.successfactors.eu",
	"glanbia,glanbiaplc,career2.successfactors.eu",
	"goldfields,C0008741144P,career5.successfactors.eu",
	"gpeastasia,greenpeace,career10.successfactors.com",
	"grace,wrgraceco,career8.successfactors.com",
	"graincorp,GraincorpProd,career10.successfactors.com",
	"grainger,Grainger,career8.successfactors.com",
	"greatlakescheese,glcheeseP,career4.successfactors.com",
	"greenstonefcs,greenstonefcs,career4.successfactors.com",
	"grenke,grenkeag,career5.successfactors.eu",
	"grifols,Grifols,career5.successfactors.eu",
	"groupebel,BEL,career5.successfactors.eu",
	"groupepomona,pomona,career5.successfactors.eu",
	"groupetf1,tfsaP,career5.successfactors.eu",
	"growmark,Growmark,career4.successfactors.com",
	"grozbeckert,grozbecker,career5.successfactors.eu",
	"gruma,asesoradee,career5.successfactors.eu",
	"grunenthal,Grunenthal,career5.successfactors.eu",
	"guerbet,guerbet,career2.successfactors.eu",
	"gulfstream,GulfStrProd,career4.successfactors.com",
	"halliburton,HALprod,career4.successfactors.com",
	"hallmark,hallmarkca,career5.successfactors.eu",
	"harleydavidson,HD,career8.successfactors.com",
	"hatch,hatchassocP,career4.successfactors.com",
	"havepurpose,advanceameP,career4.successfactors.com",
	"havertys,havertyfur,career8.successfactors.com",
	"heineken,C0000032666P,career5.successfactors.eu",
	"heliostowers,heliostoweP,career5.successfactors.eu",
	"helsinki,helsinginy,career2.successfactors.eu",
	"hendersongroup,hendersonw,career2.successfactors.eu",
	"herogroup,gaiushragP8,career2.successfactors.eu",
	"hesta,hestasuper,career10.successfactors.com",
	"hexion,Momentive,career4.successfactors.com",
	"hhcorp,thehealtha,career4.successfactors.com",
	"hisd,C0000167672P,career4.successfactors.com",
	"hkiashl,asiaworlde,career10.successfactors.com",
	"hmhco,houghtonmiP,career4.successfactors.com",
	"hoerbiger,HOEProd,career5.successfactors.eu",
	"hoermann,hrmannkgve,career2.successfactors.eu",
	"holcim,holcimgrou,career2.successfactors.eu",
	"hollister,Hollister,career4.successfactors.com",
	"homeaffairs,DIAC,career10.successfactors.com",
	"hrcampus,HRC,career5.successfactors.eu",
	"hrs,hrshotelre,career5.successfactors.eu",
	"hsitp,hongkongsh,career10.successfactors.com",
	"hubbell,Hubbell,career4.successfactors.com",
	"huber,JMHGPS,career8.successfactors.com",
	"hudbayminerals,hudbay,career4.successfactors.com",
	"hufgroup,hufhlsbeck,career5.successfactors.eu",
	"huntingtoningalls,huntingt01P2,career4.successfactors.com",
	"hydro,norskhydro,career5.successfactors.eu",
	"hysan,hysancorpo,career10.successfactors.com",
	"hyundai,hmc1,career8.successfactors.com",
	"iclgroup,ICLPROD,career2.successfactors.eu",
	"icrc,ICRCPROD,career5.successfactors.eu",
	"idelux,idelux,career2.successfactors.eu",
	"idorsia,gaiushragP4,career2.successfactors.eu",
	"ijm,ijmcorpora,career10.successfactors.com",
	"ilo,ILO,career5.successfactors.eu",
	"imd,imdinterna,career2.successfactors.eu",
	"indoramaventures,indoramaveP2,career2.successfactors.eu",
	"ineosstyrolution,ineoseurop,career2.successfactors.eu",
	"infrabel,infrabelnvP2,career2.successfactors.eu",
	"ingenico,ingenicobu,career5.successfactors.eu",
	"inspirecommunities,inspirecom,career4.successfactors.com",
	"instone,instonerea,career5.successfactors.eu",
	"internationalsos,INTLSOS,career4.successfactors.com",
	"intesasanpaolo,intesasanp,career2.successfactors.eu",
	"ioc,comitinter,career2.successfactors.eu",
	"irt,illawarrarP,career10.successfactors.com",
	"irvinecompany,irvinecompany,career4.successfactors.com",
	"issworld,issworldseP,career2.successfactors.eu",
	"iter,ITER,career5.successfactors.eu",
	"itpaero,ITP,career5.successfactors.eu",
	"ixom,C0021838914P,career10.successfactors.com",
	"jbs,jbsaustral,career10.successfactors.com",
	"jeldwen,C0000177844P,career4.successfactors.com",
	"jetblue,jetblueair,career8.successfactors.com",
	"jjkeller,C0000167916P,career4.successfactors.com",
	"jobslotusbakeries,Lotus,career5.successfactors.eu",
	"johnholland,johnhollan,career10.successfactors.com",
	"johnsonville,1972592P,career4.successfactors.com",
	"jowat,jowatse,career2.successfactors.eu",
	"jti,JTIPROD,career5.successfactors.eu",
	"karieranabank,bankpolska,career5.successfactors.eu",
	"karlstorz,karlstor01,career2.successfactors.eu",
	"karrierebeichampignon,ksereicham,career5.successfactors.eu",
	"kaufland,KAUFLAND,career5.successfactors.eu",
	"kaust,kingabdull,career2.successfactors.eu",
	"kbcgroup,C0016941732P,career5.successfactors.eu",
	"keells,johnkeells,career10.successfactors.com",
	"kellanova,KLGProduction,career4.successfactors.com",
	"kemira,kemira,career2.successfactors.eu",
	"kennametal,C0000160740P,career4.successfactors.com",
	"keolis,SIRHP,career5.successfactors.eu",
	"kernliebers,hugokernun,career5.successfactors.eu",
	"keyence,keyencec01,career4.successfactors.com",
	"kistler,kistlerins,career5.successfactors.eu",
	"kiwirail,kiwiraillt,career10.successfactors.com",
	"kmart,kmartaustr,career10.successfactors.com",
	"kmd,kmd,career5.successfactors.eu",
	"knapp,knappag,career2.successfactors.eu",
	"knorrbremse,knorrbremsP2,career5.successfactors.eu",
	"koenigbauer,koenigbaue,career5.successfactors.eu",
	"koerber,Koerber,career5.successfactors.eu",
	"komatsu,KomatsuLive,career8.successfactors.com",
	"kongsbergautomotive,kongsberg,career2.successfactors.eu",
	"kozut,magyarkztz,career2.successfactors.eu",
	"kpfilms,klcknerpenP,career5.successfactors.eu",
	"kpmg,KPMGHESPROD,career5.successfactors.eu",
	"kpmg2,KPMGHUKPROD,career5.successfactors.eu",
	"krones,kronesag,career5.successfactors.eu",
	"kronesusa,kronesinc,career8.successfactors.com",
	"ksgr,kantonsspi,career5.successfactors.eu",
	"kuritawater,kuritaeuro,career5.successfactors.eu",
	"kws,kwssaatse,career5.successfactors.eu",
	"lacare,C0014377839P,career4.successfactors.com",
	"landisgyr,LandisGyrPROD,career5.successfactors.eu",
	"lanecrawford,LaneCrawford,career10.successfactors.com",
	"langan,langan,career4.successfactors.com",
	"latecoere,LATECOERE,career5.successfactors.eu",
	"lavazza,C0001098597P,career5.successfactors.eu",
	"leggett,leggettplatt,career4.successfactors.com",
	"leonardodrs,drs,career4.successfactors.com",
	"les,C0000166626T1,career8.successfactors.com",
	"lew,rweserviceP3,career5.successfactors.eu",
	"lidl,lidlstiftuP2,career5.successfactors.eu",
	"liebherr,LiMySLive,career5.successfactors.eu",
	"life,thegreatweP3,career5.successfactors.eu",
	"lifeblood,australi02,career10.successfactors.com",
	"lightsourcebp,lightsourc,career2.successfactors.eu",
	"lincolnelectric,LEPROD,career2.successfactors.eu",
	"lindsay,C0000222018P,career4.successfactors.com",
	"linetgroup,linetgroup,career2.successfactors.eu",
	"linkreit,linkassetm,career10.successfactors.com",
	"lionco,Lion,career10.successfactors.com",
	"lionsgate,lionsgate,career4.successfactors.com",
	"litrail,jsclithuan,career2.successfactors.eu",
	"livingstonintl,LII,career4.successfactors.com",
	"lpcorp,louisianap,career8.successfactors.com",
	"lsgskychefs,lsglufthanP1,career8.successfactors.com",
	"ltts,LTTS,career10.successfactors.com",
	"lubrizol,Lubrizolprod,career4.successfactors.com",
	"lukb,LUKB,career5.successfactors.eu",
	"lundbeck,Lundbeck,career5.successfactors.eu",
	"lupin,C0001119306P,career8.successfactors.com",
	"lyondellbasell,LBI,career4.successfactors.com",
	"macmahon,macmahonho,career10.successfactors.com",
	"magairports,C0008527882P,career5.successfactors.eu",
	"mahle,mahleinter,career5.successfactors.eu",
	"malaysiaairports,malaysia01,career10.successfactors.com",
	"mapal,mapalfabri,career5.successfactors.eu",
	"mapfre,C0012595453P,career5.successfactors.eu",
	"mapletree,C0003388462P,career10.successfactors.com",
	"marturfompak,stnberkhol,career2.successfactors.eu",
	"mbie,MBIEPROD,career10.successfactors.com",
	"mccormick,McCormick,career4.successfactors.com",
	"mdc,C0007443588P,career4.successfactors.com",
	"meaamea,mitsubis07,career4.successfactors.com",
	"medartis,Medartis,career2.successfactors.eu",
	"mediamarktsaturn,mediasatur,career5.successfactors.eu",
	"medibank,Medibank,career10.successfactors.com",
	"melia,prodigiosiP,career5.successfactors.eu",
	"merckgroup,merckgroup,career5.successfactors.eu",
	"mersgoodwill,mersmissou,career4.successfactors.com",
	"metcash,metcashtra,career10.successfactors.com",
	"metrobank,MBTCHCM,career10.successfactors.com",
	"mhi,mitsubisP1,career5.successfactors.eu",
	"milcobel,C0007177194P,career5.successfactors.eu",
	"milliken,millikenco,career4.successfactors.com",
	"mindray,Mindray,career2.successfactors.eu",
	"mirgor,mirgor,career8.successfactors.com",
	"mizuhoemea,mizuhoba01,career2.successfactors.eu",
	"mizuhosi,mizuhoorthP,career4.successfactors.com",
	"mohawkind,C0014376286P,career4.successfactors.com",
	"mondigroup,mondiag,career2.successfactors.eu",
	"mosca,moscagmbh,career5.successfactors.eu",
	"motaengil,C0001190956P,career5.successfactors.eu",
	"mscdirect,MyMSC,career8.successfactors.com",
	"msgglobal,msgglobals,career5.successfactors.eu",
	"muellergroup,mllerservi,career5.successfactors.eu",
	"murata,murataamer,career8.successfactors.com",
	"musashigroup,musashibad,career5.successfactors.eu",
	"nakilat,Nakilat,career5.successfactors.eu",
	"nal,nalP,career4.successfactors.com",
	"nassco,NASSCO,career4.successfactors.com",
	"nch,nch,career4.successfactors.com",
	"ncl,newcastleu,career2.successfactors.eu",
	"nedbank,C0001228596P,career2.successfactors.eu",
	"nemak,Nemak,career5.successfactors.eu",
	"neovialogistics,3534161P,career8.successfactors.com",
	"nestle,nestleHRprdBX,career2.successfactors.eu",
	"netapp,netappinc,career4.successfactors.com",
	"newjob,hampshirecP,career5.successfactors.eu",
	"nexans,NEXANS,career2.successfactors.eu",
	"nextcenturi,C0000208733P,career4.successfactors.com",
	"nexteer,nexteer,career4.successfactors.com",
	"ngkntk,ngksparkpl,career5.successfactors.eu",
	"nibco,nibcoinc,career8.successfactors.com",
	"nomura,nomurahold,career4.successfactors.com",
	"nordangliaeducation,C0019617072P,career5.successfactors.eu",
	"normagroup,normagroupP,career5.successfactors.eu",
	"novaresteam,novaresfra,career2.successfactors.eu",
	"novocure,novocurein,career4.successfactors.com",
	"novonordisk,novonordisk,career2.successfactors.eu",
	"nrgenergy,C0004920031P,career4.successfactors.com",
	"nscorp,S003808746P,career8.successfactors.com",
	"nwnatural,northwestn,career4.successfactors.com",
	"nypaprod,NYPAPROD,career4.successfactors.com",
	"oberalp,oberalpspa,career2.successfactors.eu",
	"odysseygroup,Odyssey,career4.successfactors.com",
	"oetker,C0000030898P,career5.successfactors.eu",
	"officeworks,officework,career10.successfactors.com",
	"ofi,OLAMP1,career10.successfactors.com",
	"olamgroup,OLAM,career10.successfactors.com",
	"olympusamerica,olympuscorP,career4.successfactors.com",
	"omv,omvagPRD,career5.successfactors.eu",
	"oneline,oceannetwo,career10.successfactors.com",
	"ontex,C0002505869P,career5.successfactors.eu",
	"opecfund,opecfundfo,career2.successfactors.eu",
	"open,OUP1,career5.successfactors.eu",
	"optos,optosplc,career2.successfactors.eu",
	"orbia,mexichemP,career5.successfactors.eu",
	"orbis,orbisag,career5.successfactors.eu",
	"originenergy,Origin,career10.successfactors.com",
	"orior,Orior,career5.successfactors.eu",
	"orkla,C0001151091P,career5.successfactors.eu",
	"ormat,ormatsyste,career2.successfactors.eu",
	"ornua,Ornua,career5.successfactors.eu",
	"osram,OSRAMP,career5.successfactors.eu",
	"otb,otbspa,career2.successfactors.eu",
	"ottobock,Ottobock,career5.successfactors.eu",
	"oup,OUP,career5.successfactors.eu",
	"outokumpu,outokumpuoP,career5.successfactors.eu",
	"oxfamnovib,OxfamNovibP,career2.successfactors.eu",
	"oxinst,oxfordinst,career2.successfactors.eu",
	"paccar,paccarinc,career5.successfactors.eu",
	"pacificorp,pacificorp,career4.successfactors.com",
	"pahc,PhibroProd,career4.successfactors.com",
	"parpacific,parpacific,career4.successfactors.com",
	"partnerre,C0002079929P,career5.successfactors.eu",
	"paturnpike,pennsylvan,career4.successfactors.com",
	"pccw,hktservice,career10.successfactors.com",
	"pcl,PCLcompanies,career4.successfactors.com",
	"peelports,peelportsi,career2.successfactors.eu",
	"perfettivanmelle,PVMP,career5.successfactors.eu",
	"perrigo,Perrigo,career4.successfactors.com",
	"pestanagroup,pestanaman,career2.successfactors.eu",
	"pferd,augustrgge,career5.successfactors.eu",
	"pge,C0000161245P,career4.successfactors.com",
	"phillips66,Phillips66,career4.successfactors.com",
	"piller,pillerblow,career5.successfactors.eu",
	"pittini,compagnias,career2.successfactors.eu",
	"pkobp,powszechna,career5.successfactors.eu",
	"planinternational,PlanInt,career5.successfactors.eu",
	"popular,Popularinc,career4.successfactors.com",
	"powerco,PowerCoSEp,career5.successfactors.eu",
	"powerholdingintl,powerinter,career2.successfactors.eu",
	"pradagroup,pradaspaP2,career5.successfactors.eu",
	"precisiondrilling,precisiondril,career4.successfactors.com",
	"prezero,PreZero,career5.successfactors.eu",
	"prym,williampry,career5.successfactors.eu",
	"pse,pugetsou01,career4.successfactors.com",
	"pttep,PTTEP,career10.successfactors.com",
	"puratos,C0000032578P,career5.successfactors.eu",
	"purdue,purdueuniv,career8.successfactors.com",
	"queenslandrail,queensla01,career10.successfactors.com",
	"quintet,quintet,career5.successfactors.eu",
	"railworks,railworksc,career4.successfactors.com",
	"rch,theroyalch,career10.successfactors.com",
	"reganosa,regasifica,career2.successfactors.eu",
	"rehabellikon,rehaklinik,career2.successfactors.eu",
	"revgroup,revgroupin,career8.successfactors.com",
	"rhimagnesita,rhimagnesi,career2.successfactors.eu",
	"rich,richprod,career4.successfactors.com",
	"richardwolf,richardwol,career5.successfactors.eu",
	"rocagroup,Roca,career5.successfactors.eu",
	"rovensa,rovensa,career2.successfactors.eu",
	"royallondon,royallondo,career2.successfactors.eu",
	"rwe,rweProd,career5.successfactors.eu",
	"salzgitterag,gesisgesel,career5.successfactors.eu",
	"samsung,C0000161430P,career8.successfactors.com",
	"sap,SAP,career5.successfactors.eu",
	"sappi,sappipapie,career2.successfactors.eu",
	"sariba,saribaasT2,career5.successfactors.eu",
	"sasgroup,scandina01,career2.successfactors.eu",
	"sasol,sasolchemP3,career5.successfactors.eu",
	"sbb,C0007256361P,career5.successfactors.eu",
	"sbmoffshore,singlebuoy,career2.successfactors.eu",
	"scania,volkswagenP20,career5.successfactors.eu",
	"schaeffler,schaeffler,career5.successfactors.eu",
	"schindler,Schindler,career5.successfactors.eu",
	"schlumberger,Schlumberger,career2.successfactors.eu",
	"schott,schottag,career5.successfactors.eu",
	"schuelke,schlkemayr,career2.successfactors.eu",
	"schumacheronline,drschumach,career5.successfactors.eu",
	"schwarz,SCHWARZ,career5.successfactors.eu",
	"schwarzproduktion,MEGProd,career5.successfactors.eu",
	"sckcen,centredtud,career2.successfactors.eu",
	"sedgwickcounty,Sedgwick,career8.successfactors.com",
	"seeburger,seeburgera,career5.successfactors.eu",
	"seetec,Seetec,career5.successfactors.eu",
	"sglcarbon,sglcarbons,career5.successfactors.eu",
	"sgx,SGX,career10.successfactors.com",
	"shiseido,shiseidoco,career5.successfactors.eu",
	"shlmedical,PSshlmedical,career5.successfactors.eu",
	"sick,sickagP,career5.successfactors.eu",
	"sicpa,SICPA,career5.successfactors.eu",
	"siegwerk,Siegwerk,career5.successfactors.eu",
	"simplot,simplotP,career4.successfactors.com",
	"skf,SKF,career5.successfactors.eu",
	"skyguide,SkyguidePROD,career5.successfactors.eu",
	"skyworksinc,skyworks,career4.successfactors.com",
	"sloan,sloan,career4.successfactors.com",
	"sma,C0001121479P2,career5.successfactors.eu",
	"smbc,SMBC,career5.successfactors.eu",
	"smbc2,smbcP6,career5.successfactors.eu",
	"smbcgroup,smbcP,career5.successfactors.eu",
	"smythstoys,smythstoys,career2.successfactors.eu",
	"snopud,3437544P,career8.successfactors.com",
	"sofidel,sofidelspa,career2.successfactors.eu",
	"solvay,solvaysa,career2.successfactors.eu",
	"sonaearauco,sonaearauco,career2.successfactors.eu",
	"sonova,Sonova,career5.successfactors.eu",
	"south32,southgroupP,career10.successfactors.com",
	"spireenergy,laclede,career4.successfactors.com",
	"spx,SPX,career8.successfactors.com",
	"ssgservice,C0000955185P,career5.successfactors.eu",
	"stada,STADAAG,career5.successfactors.eu",
	"stadtzuerich,STZH,career2.successfactors.eu",
	"starhub,starhubltd,career10.successfactors.com",
	"stateofver,stateofver,career4.successfactors.com",
	"stengg,singaporet,career10.successfactors.com",
	"steriscorpp,steriscorpP,career4.successfactors.com",
	"stratasys,Stratasys,career4.successfactors.com",
	"stulz,stulzairteP,career8.successfactors.com",
	"subarusia,subaruofin,career4.successfactors.com",
	"successsolutions,2SPROD,career2.successfactors.eu",
	"sumitomocorp,SCOA,career4.successfactors.com",
	"suncommunities,suncomm,career4.successfactors.com",
	"supermicro,supermicro,career4.successfactors.com",
	"suss,sussP,career10.successfactors.com",
	"suva,suva,career2.successfactors.eu",
	"swissre,SwissRe,career2.successfactors.eu",
	"swsb,stadtwerke,career5.successfactors.eu",
	"syncreon,syncreonam,career5.successfactors.eu",
	"systematic,systematic,career2.successfactors.eu",
	"tatamotors,tataindiaP,career5.successfactors.eu",
	"te,TEConnect,career8.successfactors.com",
	"tec,ITESM,career8.successfactors.com",
	"teck,Teck,career4.successfactors.com",
	"tecoenergy,TECO,career4.successfactors.com",
	"tekasystemp,tekasystemP,career2.successfactors.eu",
	"tele2,TELE2,career2.successfactors.eu",
	"telefonica,Telefonica,career5.successfactors.eu",
	"tenaris,Tenaris,career4.successfactors.com",
	"tenneco,TennecoP1,career8.successfactors.com",
	"tenova,Techint,career5.successfactors.eu",
	"terumoamericas,Terumomedical,career4.successfactors.com",
	"terumobct,terumobct,career5.successfactors.eu",
	"tetrapak,abtetrap01,career2.successfactors.eu",
	"tfewines,tfewines,career4.successfactors.com",
	"thebicestercollection,S001955610P,career5.successfactors.eu",
	"thehersheycompany,Hersheys,career4.successfactors.com",
	"thommengroup,Thommen,career5.successfactors.eu",
	"timken,PROD,career8.successfactors.com",
	"tkelevator,C0001089563P,career5.successfactors.eu",
	"trakindo,ptmitrasol,career10.successfactors.com",
	"transalta,TransAlta,career4.successfactors.com",
	"triumphgroup,Triumph,career4.successfactors.com",
	"triviumpackaging,triviumpac,career2.successfactors.eu",
	"uc,UCPROD,career8.successfactors.com",
	"uclouvain,UCLouvain,career5.successfactors.eu",
	"uct,universi07,career2.successfactors.eu",
	"ugicorp,ugicorpo,career4.successfactors.com",
	"ukpowernetworks,4112483P,career5.successfactors.eu",
	"uksh,universi13,career2.successfactors.eu",
	"umicore,UmicorePROD,career5.successfactors.eu",
	"unesco,unesco,career2.successfactors.eu",
	"uneteaestafeta,estafeta,career4.successfactors.com",
	"uniqagroup,Uniqa,career5.successfactors.eu",
	"unisiegen,unisiegen,career5.successfactors.eu",
	"universiteitleiden,LeidenProd,career5.successfactors.eu",
	"univie,univie,career5.successfactors.eu",
	"up,UPProd,career4.successfactors.com",
	"usz,USZ,career5.successfactors.eu",
	"vaillantgroup,Vaillant,career5.successfactors.eu",
	"vailresorts,Vail,career8.successfactors.com",
	"vaisala,vaisalaoyj,career2.successfactors.eu",
	"valentino,valentinos,career2.successfactors.eu",
	"valiant,ValiantP,career2.successfactors.eu",
	"venturafoods,C0003446606P,career4.successfactors.com",
	"veolia,veoliaen01,career10.successfactors.com",
	"vetropack,vetroconsu,career5.successfactors.eu",
	"viega,viegaholdi,career2.successfactors.eu",
	"viridor,3136788P,career2.successfactors.eu",
	"vistra,vistracorp,career5.successfactors.eu",
	"vitescotechnologies,VitescoProd,career2.successfactors.eu",
	"vitra,vitraitser,career2.successfactors.eu",
	"vline,vlineeurop,career5.successfactors.eu",
	"vodafone,vodafoneprP,career5.successfactors.eu",
	"voith,VOITH,career5.successfactors.eu",
	"volaris,volaris,career4.successfactors.com",
	"volkswagen,volkswag04,career10.successfactors.com",
	"volkswagengroup,VWAGLPPROD10,career5.successfactors.eu",
	"volvocars,C0000870892P,career5.successfactors.eu",
	"vonovia,vonoviase,career5.successfactors.eu",
	"vub,VUB,career2.successfactors.eu",
	"wacker,wackerchem,career5.successfactors.eu",
	"wagnergroup,jwagnergmb,career2.successfactors.eu",
	"warburtons,warburtons,career2.successfactors.eu",
	"wartsila,Wartsila,career2.successfactors.eu",
	"wcbradley,wcbradley,career4.successfactors.com",
	"werkenbijvitens,vitensnv,career2.successfactors.eu",
	"westernpower,WesternPower,career10.successfactors.com",
	"westfalen,westfalena,career5.successfactors.eu",
	"westinghousenuclear,WestinghouseP,career4.successfactors.com",
	"westpharma,C0000160857P,career5.successfactors.eu",
	"whirlpool,Whirlpool,career4.successfactors.com",
	"wilsongroupau,wilsongroup,career10.successfactors.com",
	"winbond,winbondele,career10.successfactors.com",
	"witglobal,wrthitinteP2,career5.successfactors.eu",
	"worldline,Worldline,career2.successfactors.eu",
	"wwz,WWZ,career5.successfactors.eu",
	"wyndhamhotels,Wyndham,career4.successfactors.com",
	"yamahamotor,C0000161306P,career5.successfactors.eu",
	"yancoal,yancoalaus,career10.successfactors.com",
	"yara,yaraintern,career2.successfactors.eu",
	"yarratrams,kdrvictori,career10.successfactors.com",
	"yash,yashtechnoP,career10.successfactors.com",
	"ykkap,ykkapameri,career4.successfactors.com",
	"yunextraffic,YunexPROD,career2.successfactors.eu",
	"zalaris,ZalarisProd,career2.successfactors.eu",
	"zf,zffriedric,career5.successfactors.eu",
	"zimmerin01,zimmerin01,career8.successfactors.com",
	"zimvie,741838241,career8.successfactors.com",
	"zorgbedrijf,Zorgbedrijf,career2.successfactors.eu",
	"zurich,SF2013,career2.successfactors.eu",
	"zurzachcare,ZURZACHCare,career5.successfactors.eu",
}

// successFactorsTenant is one parsed entry of [SuccessFactorsTenants].
type successFactorsTenant struct {
	// key is the entry exactly as registered, which is what [Source.Key] and
	// [internal.PostingSource.Key] carry. Kept verbatim rather than rebuilt from
	// the parts below, so the identity a posting reports is the one a person can
	// paste back into --company.
	key string

	// slug is this project's name for the employer, and the only part of the
	// triple a person ever types.
	slug string

	// companyID is the ?company= value, case-sensitive.
	companyID string

	// host is the career{N}.successfactors.{com|eu} host serving this tenant.
	host string
}

// parseSuccessFactorsTenant splits a "slug,companyId,host" key.
//
// A malformed entry is an error rather than a best-effort guess. The three parts
// are independent facts about a tenant that cannot be derived from each other,
// so a two-part key is not a tenant missing a default, it is a mis-transcribed
// line — and building a URL from it would produce a request that fails somewhere
// far away from the typo.
func parseSuccessFactorsTenant(key string) (successFactorsTenant, error) {
	parts := strings.Split(key, ",")
	if len(parts) != 3 {
		return successFactorsTenant{}, fmt.Errorf("invalid SuccessFactors tenant %q: want %q", key, "slug,companyId,host")
	}

	tenant := successFactorsTenant{
		key:       key,
		slug:      strings.TrimSpace(parts[0]),
		companyID: strings.TrimSpace(parts[1]),
		host:      strings.TrimSpace(parts[2]),
	}

	if tenant.slug == "" || tenant.companyID == "" || tenant.host == "" {
		return successFactorsTenant{}, fmt.Errorf("invalid SuccessFactors tenant %q: want %q with all three parts set", key, "slug,companyId,host")
	}

	return tenant, nil
}

// successFactorsCompanyName derives the display name from a tenant triple: the
// slug, which is the first field.
//
// It returns the key unchanged when the triple is malformed, so a bad entry
// stays traceable back to the line that produced it rather than becoming an
// empty name in the company list — the same choice [workdayCompanyName] makes.
func successFactorsCompanyName(key string) string {
	tenant, err := parseSuccessFactorsTenant(key)
	if err != nil {
		return key
	}

	return tenant.slug
}

// successFactorsFeedMarker is the root element of the RMK listing feed.
//
// Its presence is what tells a real feed apart from the small HTML page a wrong
// host or company id answers with. That page is served with a 200, so status
// code alone cannot make the distinction, and treating it as an empty board
// would turn every mis-transcribed tenant into a silently-empty source.
const successFactorsFeedMarker = "<Job-Listing"

// The RMK feed is deliberately scanned rather than parsed with encoding/xml.
//
// It is not strict XML: the runtime emits empty "<>...</>" tags for facets a
// tenant has not configured, which encoding/xml rejects outright. One such tag
// anywhere in the document would cost the whole tenant its postings, which is
// the silently-empty source this project treats as its worst failure. A scanner
// that looks only for the elements it needs cannot be broken by markup it does
// not read.
//
// Every pattern is non-greedy and anchored on a full tag name, so <Job> does not
// match <Job-Description> or <JobTitle>, and (?s) is what lets a CDATA
// description spanning thousands of lines be captured as one value.
var (
	successFactorsJobPattern         = regexp.MustCompile(`(?is)<Job\s*>(.*?)</Job\s*>`)
	successFactorsTitlePattern       = regexp.MustCompile(`(?is)<JobTitle\b[^>]*>(.*?)</JobTitle\s*>`)
	successFactorsDescriptionPattern = regexp.MustCompile(`(?is)<Job-Description\b[^>]*>(.*?)</Job-Description\s*>`)
	successFactorsReqIDPattern       = regexp.MustCompile(`(?is)<ReqId\b[^>]*>(.*?)</ReqId\s*>`)
	successFactorsPostedPattern      = regexp.MustCompile(`(?is)<Posted-Date\b[^>]*>(.*?)</Posted-Date\s*>`)

	// Facets are the tenant-variable part of the feed: each configured filter or
	// multi-value field arrives as <filterN>/<mfieldN> carrying a <label> and a
	// <value>, and which N holds the location differs per tenant. Matching the
	// family rather than a fixed number is the only way to read them without a
	// per-tenant mapping table.
	successFactorsFacetPattern = regexp.MustCompile(`(?is)<(?:filter|mfield)\d*\s*>(.*?)</(?:filter|mfield)\d*\s*>`)
	successFactorsLabelPattern = regexp.MustCompile(`(?is)<label\b[^>]*>(.*?)</label\s*>`)
	successFactorsValuePattern = regexp.MustCompile(`(?is)<value\b[^>]*>(.*?)</value\s*>`)

	// successFactorsMergeToken matches an RMK merge token such as
	// "[[salaryMin]]" or "[[filter1]]". These are placeholders the career site's
	// client-side runtime would have substituted; a plain HTTP client receives
	// them literally, and publishing one as a location or a title would put
	// template syntax in front of a job seeker.
	successFactorsMergeToken = regexp.MustCompile(`\[\[[^\]\[]*\]\]`)
)

// successFactorsText renders one captured element's contents as plain text.
//
// CDATA is unwrapped and its contents are left exactly as they arrived: CDATA
// exists precisely to hold text that must not be re-interpreted, so unescaping
// it would turn a literal "&amp;" in a job title into "&". A value that is not
// CDATA-wrapped is entity-decoded, because for those the encoding is real.
func successFactorsText(raw string) string {
	text := strings.TrimSpace(raw)

	if after, ok := strings.CutPrefix(text, "<![CDATA["); ok {
		text = strings.TrimSuffix(after, "]]>")
	} else {
		text = html.UnescapeString(text)
	}

	text = successFactorsMergeToken.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

// successFactorsElement returns the text of the first element matched by pattern
// within one <Job> block, or "" when the element is absent.
func successFactorsElement(pattern *regexp.Regexp, block string) string {
	match := pattern.FindStringSubmatch(block)
	if match == nil {
		return ""
	}

	return successFactorsText(match[1])
}

// successFactorsFacet is one label/value pair from a <filterN> or <mfieldN>
// wrapper.
type successFactorsFacet struct {
	label string
	value string
}

// successFactorsFacets reads every configured facet out of one <Job> block, in
// document order.
func successFactorsFacets(block string) []successFactorsFacet {
	matches := successFactorsFacetPattern.FindAllStringSubmatch(block, -1)

	facets := make([]successFactorsFacet, 0, len(matches))

	for _, match := range matches {
		facet := successFactorsFacet{
			label: strings.ToLower(successFactorsElement(successFactorsLabelPattern, match[1])),
			value: successFactorsElement(successFactorsValuePattern, match[1]),
		}

		// A facet the tenant left unconfigured arrives with an empty value, and
		// one with no label cannot be identified at all; neither is usable, and
		// keeping them would only make the lookups below scan further.
		if facet.label == "" || facet.value == "" {
			continue
		}

		facets = append(facets, facet)
	}

	return facets
}

// Facet labels are matched by substring, in the priority order given here.
//
// Which facet holds which fact is a per-tenant configuration choice, and there is
// nothing in the feed that says which convention a tenant follows: measured live
// on 2026-07-28, CRH labels its geography "Country" and "State/Province/County",
// Colgate labels it "Country", "State/Province" and "City", and Zurich labels it
// "Country of Search". Matching on the label the tenant chose to display is the
// only signal available.
//
// This is the least certain part of the adapter and it is deliberately the least
// load-bearing: a label that matches nothing here leaves an enrichment field
// empty, exactly as [internal.NormalizeEmploymentType] returning false does.
// Title, URL and the posting itself never depend on any of it.
var (
	// successFactorsWorkplaceLabels also acts as an exclusion list for
	// locations: "Location Flexibility" is a remote/hybrid picklist and contains
	// the word "location", so a plain substring search would file "Hybrid" as a
	// city.
	//
	// Deliberately WITHOUT "work location", which is the trap this list cannot
	// be the answer to. Colgate labels its remote/hybrid picklist "Work
	// Location", but of the eight tenants measured on 2026-07-28 that carry a
	// "... Location" label of that family, seven use it for real geography:
	// Cornell publishes "Upper East Side", Schindler "Boston", Voith
	// "Heidenheim, BW (DE)", Langan "Arlington, VA", Cincinnati "Main Campus".
	// Excluding the label would have cost those seven their locations to fix one
	// tenant. [successFactorsWorkplaceValue] handles Colgate by reading the
	// value instead, which is where the two cases actually differ.
	successFactorsWorkplaceLabels = []string{"work model", "workplace", "work arrangement", "location flexibility", "remote"}

	// Ordered most-complete-first: a tenant that publishes both "Location" and
	// "City" usually puts the fuller string in the former ("Ludwigshafen, DE"
	// against "Ludwigshafen"), and 46 of the 739 live tenants publish a bare
	// "Location" facet.
	successFactorsLocationLabels = []string{"geographic location", "location", "city", "region", "state", "province", "country"}

	// "job area" is Zurich's label for what every other measured tenant calls a
	// job function ("Claims", "Underwriting", "Information Technology"); five
	// live tenants use it, and no measured tenant uses it for geography.
	successFactorsDepartmentLabels = []string{"job function", "function", "department", "job family", "job category", "career area", "job area"}

	// Deliberately without "job type": Zurich publishes a "Job Type" facet whose
	// values are seniority levels ("Experienced", "Entry", "Graduate"), not
	// employment types, so matching it would file a seniority as a contract
	// shape on every posting that has one.
	successFactorsEmploymentLabels = []string{"employment type", "employment status", "contract type", "work schedule", "employment"}
)

// successFactorsFacetValue returns the value of the first facet whose label
// contains one of labels, honouring the order of labels rather than the order of
// the facets, so priority is a property of this file and not of a tenant's
// column layout.
func successFactorsFacetValue(facets []successFactorsFacet, labels []string, exclude []string) string {
	for _, want := range labels {
		for _, facet := range facets {
			if !strings.Contains(facet.label, want) {
				continue
			}

			if successFactorsLabelContains(facet.label, exclude) {
				continue
			}

			return facet.value
		}
	}

	return ""
}

// successFactorsSquash reduces a facet value to comparable letters and digits,
// the same shape [internal.NormalizeWorkplaceType] compares internally.
func successFactorsSquash(value string) string {
	var builder strings.Builder

	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// successFactorsWorkplaceValue reports whether a facet's value is itself a
// workplace answer rather than a place, and which one.
//
// This is the load-bearing half of telling a location facet from a
// remote/hybrid picklist, and it works on the value because the label does not
// distinguish them: measured live on 2026-07-28, "Work Location" holds "Hybrid"
// on Colgate and "Boston" on Schindler. What a picklist always has, and a place
// never does, is that the whole value IS the workplace word.
//
// The match is therefore exact rather than the substring test
// [internal.NormalizeWorkplaceType] does on its own. That distinction is the
// safety: a real location reading "Home Office, Berlin" or "Head Office"
// contains "office" and would be thrown away by a substring test, while
// "Hybrid", "Remote" and "On-site" — the three values Colgate publishes —
// reduce exactly to the canonical name and nothing else does.
func successFactorsWorkplaceValue(value string) (internal.WorkplaceType, bool) {
	workplace, ok := internal.NormalizeWorkplaceType(value)
	if !ok {
		return internal.WorkplaceTypeUnknown, false
	}

	if successFactorsSquash(value) != successFactorsSquash(string(workplace)) {
		return internal.WorkplaceTypeUnknown, false
	}

	return workplace, true
}

// successFactorsWorkplace returns the workplace type a tenant published, from
// whichever facet actually carries one.
//
// The label lookup runs first so a tenant that names its picklist explicitly
// keeps deciding; failing that, any facet whose value is a bare workplace answer
// supplies it. The fallback is what reads Colgate, whose picklist is labelled
// "Work Location" and so matches no workplace label at all — 307 of its 336
// live postings carry a workplace type that was previously dropped.
func successFactorsWorkplace(facets []successFactorsFacet) (internal.WorkplaceType, bool) {
	if workplace, ok := internal.NormalizeWorkplaceType(successFactorsFacetValue(facets, successFactorsWorkplaceLabels, nil)); ok {
		return workplace, true
	}

	for _, facet := range facets {
		if workplace, ok := successFactorsWorkplaceValue(facet.value); ok {
			return workplace, true
		}
	}

	return internal.WorkplaceTypeUnknown, false
}

// successFactorsLocation returns the place a posting is offered at.
//
// A facet whose value is a bare workplace answer is skipped however it is
// labelled: that is the whole of the Colgate correction, and it is done on the
// value so the seven measured tenants whose "... Location" facet holds a real
// city keep theirs.
func successFactorsLocation(facets []successFactorsFacet) string {
	usable := make([]successFactorsFacet, 0, len(facets))

	for _, facet := range facets {
		if _, isWorkplace := successFactorsWorkplaceValue(facet.value); isWorkplace {
			continue
		}

		usable = append(usable, facet)
	}

	return successFactorsFacetValue(usable, successFactorsLocationLabels, successFactorsWorkplaceLabels)
}

// successFactorsLabelContains reports whether a lowercased facet label contains
// any of the given words.
func successFactorsLabelContains(label string, words []string) bool {
	for _, word := range words {
		if strings.Contains(label, word) {
			return true
		}
	}

	return false
}

// successFactorsISOLayouts are the unambiguous date spellings accepted for
// <Posted-Date>, tried before any slash-separated reading.
var successFactorsISOLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// successFactorsMonthFirst is the documented spelling of <Posted-Date>:
// MM/DD/YYYY.
const successFactorsMonthFirst = "01/02/2006"

// successFactorsDayFirst is the same date read the other way round.
const successFactorsDayFirst = "02/01/2006"

// successFactorsSlashLayout decides how to read this feed's slash-separated
// dates, from the whole feed rather than from one posting.
//
// 03/04/2026 is the third of April to half the world and the fourth of March to
// the other half, and [phenomPostedAt] refuses slash dates outright for exactly
// that reason. RMK is a narrower case: the format is documented as MM/DD/YYYY,
// and a feed carries every open req at once, so the corpus itself settles the
// question. A single value whose first component exceeds 12 can only be a day,
// which proves the tenant is day-first; across the ~144 postings a typical
// tenant publishes, a day-first tenant is very unlikely to hide.
//
// Falling back to the documented reading when no such value appears is the
// remaining risk, and it is bounded: it can only be wrong for a tenant that is
// day-first AND published nothing after the 12th of any month.
func successFactorsSlashLayout(dates []string) string {
	for _, date := range dates {
		first, _, ok := strings.Cut(date, "/")
		if !ok {
			continue
		}

		// Read as a number rather than with time.Parse: the layout "01" demands
		// two digits, so an unpadded "3/04/2026" would fail to parse and be
		// mistaken for proof of a day-first tenant.
		number, err := strconv.Atoi(strings.TrimSpace(first))
		if err != nil {
			continue
		}

		// Only a day can exceed 12, so one such value settles the whole feed.
		if number > 12 {
			return successFactorsDayFirst
		}
	}

	return successFactorsMonthFirst
}

// successFactorsPostedAt converts one <Posted-Date> to UTC using the layout
// [successFactorsSlashLayout] settled on for this feed, reporting false when the
// field is absent or in a spelling this does not know.
func successFactorsPostedAt(raw, slashLayout string) (time.Time, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}, false
	}

	for _, layout := range successFactorsISOLayouts {
		if posted, err := time.Parse(layout, text); err == nil {
			return posted.UTC(), true
		}
	}

	if posted, err := time.Parse(slashLayout, text); err == nil {
		return posted.UTC(), true
	}

	return time.Time{}, false
}

// successFactorsFeed fetches one tenant's listing feed as text.
//
// It does not go through [fetchJSON]: the response is XML, and it has to be held
// as a whole because the scanner needs the complete document. The body is closed
// before this returns on every path, so a failed read cannot leave a connection
// pinned for the rest of the crawl.
func successFactorsFeed(ctx context.Context, httpClient *http.Client, tenant successFactorsTenant) (string, error) {
	feedURL := fmt.Sprintf("https://%s/career?company=%s&career_ns=job_listing_summary&resultType=XML",
		tenant.host, url.QueryEscape(tenant.companyID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request for SuccessFactors company %q at %s: %w", tenant.slug, feedURL, err)
	}

	// The feed is served as application/octet-stream by most tenants rather than
	// as any XML media type, so this is an honest statement of what is accepted
	// rather than a filter the server is expected to honour.
	req.Header.Set("Accept", "application/xml, text/xml, */*")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request to SuccessFactors for company %q at %s: %w", tenant.slug, feedURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from SuccessFactors for company %q at %s: %s", tenant.slug, feedURL, resp.Status)
	}

	// One byte past the limit is read on purpose: it is what distinguishes a feed
	// that exactly fills the budget from one that was cut short.
	body, err := io.ReadAll(io.LimitReader(resp.Body, successFactorsMaxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read response from SuccessFactors for company %q at %s: %w", tenant.slug, feedURL, err)
	}

	if len(body) > successFactorsMaxResponseBytes {
		return "", fmt.Errorf("refusing to read more than %d bytes from SuccessFactors for company %q at %s: the feed did not end, so its postings cannot be read without truncating them",
			successFactorsMaxResponseBytes, tenant.slug, feedURL)
	}

	return string(body), nil
}

// successFactorsApplyURL builds the public posting URL for a requisition.
//
// RMK publishes no per-posting link in the feed, but the application route is
// the same three parameters on the same host for every tenant, so the URL is
// synthesizable with no second request. That is the whole reason this platform
// costs one request per employer instead of one per posting.
func successFactorsApplyURL(tenant successFactorsTenant, reqID string) string {
	return fmt.Sprintf("https://%s/career?company=%s&career_job_req_id=%s&career_ns=job_application",
		tenant.host, url.QueryEscape(tenant.companyID), url.QueryEscape(reqID))
}

// SuccessFactors returns all of the job postings for one SAP SuccessFactors RMK
// tenant, or an error if there was a problem making the request or reading the
// feed.
//
// company is a "slug,companyId,host" triple, see [SuccessFactorsTenants]; it is
// not a board slug like most platforms here.
func SuccessFactors(ctx context.Context, httpClient *http.Client, company string) internal.Jobs {
	return func(yield func(*internal.JobPosting, error) bool) {
		tenant, err := parseSuccessFactorsTenant(company)
		if err != nil {
			yield(nil, err)

			return
		}

		body, err := successFactorsFeed(ctx, httpClient, tenant)
		if err != nil {
			yield(nil, err)

			return
		}

		// A wrong host or company id answers 200 with a short HTML page, so the
		// root element is the only thing that distinguishes "this tenant has no
		// open reqs" from "this tenant does not exist". Failing loudly here is
		// what keeps a mis-transcribed triple from looking like an employer with
		// nothing to offer.
		if !strings.Contains(body, successFactorsFeedMarker) {
			yield(nil, fmt.Errorf("unexpected response from SuccessFactors for company %q (company id %q on %s): no %s element, which is what a wrong host or company id answers with",
				tenant.slug, tenant.companyID, tenant.host, successFactorsFeedMarker))

			return
		}

		blocks := successFactorsJobPattern.FindAllStringSubmatch(body, -1)
		if len(blocks) == 0 {
			// The feed is real and lists nothing. An enterprise with no open
			// reqs is unusual but not an error, and the marker above has already
			// ruled out the failure that looks like this one.
			return
		}

		dates := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if posted := successFactorsElement(successFactorsPostedPattern, block[1]); posted != "" {
				dates = append(dates, posted)
			}
		}

		slashLayout := successFactorsSlashLayout(dates)

		var yielded int

		for _, block := range blocks {
			if ctx.Err() != nil {
				yield(nil, ctx.Err())

				return
			}

			posting := successFactorsJob(tenant, block[1], slashLayout)
			if posting == nil {
				continue
			}

			yielded++

			if !yield(posting, nil) {
				return
			}
		}

		// Every element name this scanner looks for came from documentation and
		// from other people's implementations, never from a response decoded
		// here. If RMK renames <JobTitle> or <ReqId>, or a tenant serves a shape
		// nobody has seen, the failure mode without this check is a tenant that
		// reports success and contributes nothing — the exact shape of failure
		// that cost this project two thirds of its coverage before. So a feed
		// that listed jobs and yielded none is an error, loudly, naming the
		// tenant.
		if yielded == 0 {
			yield(nil, fmt.Errorf("failed to read any posting from SuccessFactors for company %q (company id %q on %s): the feed listed %d jobs but none carried both a title and a requisition id, so its layout may have changed",
				tenant.slug, tenant.companyID, tenant.host, len(blocks)))
		}
	}
}

// successFactorsJob builds one posting from a <Job> block, returning nil when
// the block carries too little to be a posting.
func successFactorsJob(tenant successFactorsTenant, block, slashLayout string) *internal.JobPosting {
	var (
		title = successFactorsElement(successFactorsTitlePattern, block)
		reqID = successFactorsElement(successFactorsReqIDPattern, block)
	)

	// Without the requisition id there is no link to the posting, and this
	// project's contract is that every posting carries a URL a person can open.
	if title == "" || reqID == "" {
		return nil
	}

	facets := successFactorsFacets(block)

	location := successFactorsLocation(facets)
	if location == "" {
		location = "unknown/remote"
	}

	posting := &internal.JobPosting{
		Company:  tenant.slug,
		URL:      successFactorsApplyURL(tenant, reqID),
		Title:    title,
		Location: location,

		Department: successFactorsFacetValue(facets, successFactorsDepartmentLabels, nil),

		// RMK publishes one identifier, and it is both: the number the employer
		// quotes in an internal system and the key the application route is
		// addressed by. Filling only one of the two fields would make callers
		// guess which, so both carry it and the doc comments on
		// [internal.JobPosting] explain what each means.
		RequisitionID: reqID,
		ExternalID:    reqID,

		Source: internal.PostingSource{
			Platform: successFactorsPlatform,
			Key:      tenant.key,
		},
	}

	if employment, ok := internal.NormalizeEmploymentType(successFactorsFacetValue(facets, successFactorsEmploymentLabels, nil)); ok {
		posting.EmploymentType = employment
	}

	if workplace, ok := successFactorsWorkplace(facets); ok {
		posting.WorkplaceType = workplace
	}

	if posted, ok := successFactorsPostedAt(successFactorsElement(successFactorsPostedPattern, block), slashLayout); ok {
		posting.PostedAt = posted
	}

	// The description is already on the wire — it is the bulk of this feed — so
	// reading a pay range out of it costs no request. It arrives as HTML, which
	// [internal.ParseCompensationFromDescription] handles: it strips tags and
	// decodes entities before looking for figures. Anything it finds is marked
	// [internal.ProvenanceDescription], never confused with an employer-published
	// field, and RMK publishes no structured pay field for it to displace.
	posting.Compensation = internal.ParseCompensationFromDescription(successFactorsElement(successFactorsDescriptionPattern, block))

	return posting
}
