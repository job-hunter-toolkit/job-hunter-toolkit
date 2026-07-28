package shard

import (
	"net/url"
	"strings"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
)

// Affinity key namespaces.
//
// The prefix is not decoration. It keeps a platform name from ever colliding
// with a service key, so "greenhouse" (a platform we could not resolve to a
// host offline) and a hypothetical backend literally named "greenhouse" cannot
// be folded into one group by accident.
const (
	servicePrefix  = "service:"
	platformPrefix = "platform:"
)

// AffinityKey identifies the backend that a source's requests contend for.
//
// Sources that share an affinity key MUST run in the same shard. Every shard is
// a separate process with its own [httpx] limiter, so splitting one backend
// across two shards does not halve its wall time: it doubles the concurrent
// requests that backend sees, because each process runs a private copy of that
// service's 4-slot budget and its 429 cooldown. That is the specific thing
// docs/architecture-roadmap.md forbids ("Higher concurrency and more IPs are
// NOT permission to increase pressure on a shared service"), and it is the
// incident behind the shared limiter keys in httpx.servicePolicyFor: 56
// Workable boards were rate-limited into looking dead when each tenant got its
// own budget.
type AffinityKey string

// Platform reports whether the key is a whole-platform group, which is the
// conservative fallback used when a source's backend cannot be identified
// without making a request.
func (k AffinityKey) Platform() bool {
	return strings.HasPrefix(string(k), platformPrefix)
}

// String returns the key as written into a plan.
func (k AffinityKey) String() string { return string(k) }

// AffinityKeys returns one [AffinityKey] per source, in the same order.
//
// Affinity is derived offline, because the planner runs before any crawl and a
// source's real host is only known once a request is made. Two rules produce
// it, in order of preference:
//
//  1. If every source on a platform carries an identifiable hostname in its ATS
//     key, each source's key is httpx's limiter key for that host. This is the
//     authority the roadmap asks for rather than a second opinion: httpx already
//     collapses *.peopleforce.io, *.bamboohr.com, *.jibeapply.com and the SMB
//     subdomain platforms onto one key each, so a platform that hands out tenant
//     subdomains of a shared backend still comes back as a single group, while
//     Workday's 216 genuinely separate tenant hosts and SuccessFactors' pods
//     come back as separate groups. That is where the parallelism comes from.
//
//  2. Otherwise the whole platform is one group.
//
// Rule 1 is applied per platform, all or nothing. A platform whose ATS keys are
// board slugs ("stripe", "2k") tells us nothing about its host, and Greenhouse's
// 647 slugs all resolve to boards-api.greenhouse.io. Deciding per source would
// mean one slug that happens to contain a dot — a plausible future board name —
// silently promoted itself out of its platform's group and got crawled from a
// second runner against the same shared backend. Over-grouping only costs
// parallelism; under-grouping breaks an invariant, so the ambiguous case loses.
//
// perHostLimit is passed through to [httpx.ServicePolicyForHost] for fidelity;
// the limiter key it returns does not depend on it.
func AffinityKeys(sources []services.Source, perHostLimit int) []AffinityKey {
	keys := make([]AffinityKey, len(sources))
	resolvable := map[string]bool{}

	for i, source := range sources {
		host := hostFromSourceKey(source.Key)
		if host == "" {
			resolvable[source.Platform] = false

			continue
		}

		if _, seen := resolvable[source.Platform]; !seen {
			resolvable[source.Platform] = true
		}

		keys[i] = AffinityKey(servicePrefix + httpx.ServicePolicyForHost(host, perHostLimit).Key)
	}

	for i, source := range sources {
		if !resolvable[source.Platform] {
			keys[i] = AffinityKey(platformPrefix + source.Platform)
		}
	}

	return keys
}

// SplittablePlatforms returns the platforms whose sources resolve to more than
// one affinity key, meaning the planner may spread them over several shards.
//
// It exists to be asserted on. Whether a platform is splittable is a politeness
// decision, and deriving it from ATS key shapes means a new platform can change
// the answer without anyone deciding to. A test over the real registry turns
// that into a build failure instead of a silent doubling of some backend's
// request rate.
func SplittablePlatforms(sources []services.Source, perHostLimit int) []string {
	var (
		keys       = AffinityKeys(sources, perHostLimit)
		byPlatform = map[string]map[AffinityKey]struct{}{}
	)

	for i, source := range sources {
		if byPlatform[source.Platform] == nil {
			byPlatform[source.Platform] = map[AffinityKey]struct{}{}
		}

		byPlatform[source.Platform][keys[i]] = struct{}{}
	}

	var splittable []string

	for platform, distinct := range byPlatform {
		if len(distinct) > 1 {
			splittable = append(splittable, platform)
		}
	}

	sortStrings(splittable)

	return splittable
}

// hostFromSourceKey extracts the hostname an ATS key names, or "" when the key
// does not identify a host on its own.
//
// Three key shapes exist in the registry today: a full tenant URL (Workday,
// "https://3m.wd1.myworkdayjobs.com/Search"), a bare hostname (Phenom,
// "careers.conagrabrands.com"), and a comma-separated tuple that packs the
// tenant host into one field (Oracle Cloud HCM,
// "kroger,eluq.fa.us2.oraclecloud.com,CX_2001"; SuccessFactors,
// "crh,CRH,career2.successfactors.eu"). Everything else is a board slug.
func hostFromSourceKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	if strings.Contains(key, "://") {
		parsed, err := url.Parse(key)
		if err != nil || parsed.Hostname() == "" {
			// A key that looks like a URL but does not parse is exactly the case
			// where guessing is worst, so decline and let the platform fall back
			// to a single group.
			return ""
		}

		return strings.ToLower(parsed.Hostname())
	}

	for _, field := range strings.FieldsFunc(key, isKeySeparator) {
		if looksLikeHostname(field) {
			return strings.ToLower(field)
		}
	}

	return ""
}

func isKeySeparator(r rune) bool {
	return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// looksLikeHostname reports whether field is a dotted DNS name.
//
// It is deliberately strict. A false positive promotes one source out of its
// platform's group, and under rule 1 of [AffinityKeys] the whole platform then
// depends on every other key also resolving; a false negative only costs
// parallelism.
func looksLikeHostname(field string) bool {
	if field == "" || strings.ContainsAny(field, "/:@?#_ ") {
		return false
	}

	labels := strings.Split(field, ".")
	if len(labels) < 2 {
		return false
	}

	for _, label := range labels {
		if label == "" {
			return false
		}

		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z',
				r >= 'A' && r <= 'Z',
				r >= '0' && r <= '9',
				r == '-':
			default:
				return false
			}
		}
	}

	// A trailing all-numeric or single-character label is a version suffix or an
	// IP fragment, not a public hostname.
	last := labels[len(labels)-1]
	if len(last) < 2 {
		return false
	}

	for _, r := range last {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') {
			return false
		}
	}

	return true
}
