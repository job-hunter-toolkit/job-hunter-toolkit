package services

import (
	"context"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/shoenig/test"
)

// recordingTransport captures the first request a source makes and then fails,
// so a source can be asked "which host do you talk to?" without a network.
type recordingTransport struct {
	host string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.host == "" {
		r.host = req.URL.Host
	}

	// An empty body ends every adapter quickly: JSON decoders report a syntax
	// error and HTML parsers find no postings. Either way the source stops
	// after its first request, which is all this test needs.
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     http.StatusText(http.StatusOK),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// hostFor reports the host a source's first request goes to.
func hostFor(t *testing.T, source Source) string {
	t.Helper()

	transport := &recordingTransport{}
	client := &http.Client{Transport: transport}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	for range source.Jobs(ctx, client) {
		// One request is enough to learn the host; stopping here also exercises
		// the iterator's early-stop path for every registered source, which is
		// how an adapter that ignores yield's return value shows up.
		break
	}

	return transport.host
}

// genericPolicyPlatforms are the platforms deliberately left on the generic
// exact-host policy, with the reason each one is safe there.
//
// Anything not listed must match an explicit arm of httpx.servicePolicyFor.
var genericPolicyPlatforms = map[string]string{
	"workday": "genuinely tenant-isolated: 216 unrelated employer hosts, and sharing " +
		"a key would throttle them to four requests between them",
	"phenom": "tenant-isolated employer hostnames, same reasoning as Workday",
	"direct": "the bespoke internal/companies adapters, each of which talks to one " +
		"employer's own careers site (oxide.computer, uber.com); there is no shared " +
		"backend for tenants to contend over",
}

// TestEveryPlatformHasAPacingPolicy is a regression test for a whole class of
// omission rather than for one bug.
//
// Six platforms and 200 sources were added in a single change, and every one of
// them fell through to the generic exact-host policy, which leaves the pacing
// interval at zero. Nothing failed: the existing limiter test is a hardcoded
// table of four known platforms, so it passed vacuously for every platform that
// did not exist when it was written, and would have kept passing for every
// platform added afterwards. The gap was found by a human reading a diff, which
// is not a control.
//
// This test derives its subject list from the registry instead, so a new
// platform cannot be registered without either getting a policy or being
// explicitly and visibly excused above.
func TestEveryPlatformHasAPacingPolicy(t *testing.T) {
	t.Parallel()

	// One representative source per platform: the policy is a property of the
	// platform's hostname shape, and running all ~2,100 sources would buy
	// nothing but wall time.
	representative := map[string]Source{}
	for _, source := range Builtin {
		if _, ok := representative[source.Platform]; !ok {
			representative[source.Platform] = source
		}
	}

	platforms := make([]string, 0, len(representative))
	for platform := range representative {
		platforms = append(platforms, platform)
	}

	sort.Strings(platforms)

	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			host := hostFor(t, representative[platform])
			if host == "" {
				t.Skipf("%s made no request against a stub transport", platform)
			}

			policy := httpx.ServicePolicyForHost(host, httpx.DefaultPerHostLimit)

			if reason, excused := genericPolicyPlatforms[platform]; excused {
				test.Zero(t, policy.Interval,
					test.Sprintf("%s is excused from an explicit policy (%s) but now has pacing; "+
						"if that is deliberate, remove it from genericPolicyPlatforms", platform, reason))

				return
			}

			test.NotEq(t, 0, policy.Interval,
				test.Sprintf("platform %s requests %s, which matches no arm of "+
					"httpx.servicePolicyFor, so its requests are unpaced. Add a policy for it, "+
					"or add it to genericPolicyPlatforms with the reason it is safe unpaced.",
					platform, host))
		})
	}
}

// TestSubdomainPerTenantPlatformsShareALimiterKey checks the other half of the
// contract: a platform that gives every tenant a subdomain of one backend must
// collapse onto a single limiter key.
//
// Keying those per exact host is what let 56 Workable boards rate-limit each
// other into looking dead, so the shape is worth asserting from the registry
// rather than from a fixed list.
func TestSubdomainPerTenantPlatformsShareALimiterKey(t *testing.T) {
	t.Parallel()

	// Platforms whose tenants live at {tenant}.{backend}, where the backend is
	// one service and must therefore be one limiter key.
	sharedBackends := []string{"peopleforce", "bamboohr", "teamtailor", "recruitee", "pinpoint", "personio"}

	for _, platform := range sharedBackends {
		t.Run(platform, func(t *testing.T) {
			var hosts []string

			for _, source := range Builtin {
				if source.Platform == platform && len(hosts) < 3 {
					if host := hostFor(t, source); host != "" {
						hosts = append(hosts, host)
					}
				}
			}

			if len(hosts) < 2 {
				t.Skipf("%s has fewer than two resolvable sources registered", platform)
			}

			first := httpx.ServicePolicyForHost(hosts[0], httpx.DefaultPerHostLimit)

			for _, host := range hosts[1:] {
				policy := httpx.ServicePolicyForHost(host, httpx.DefaultPerHostLimit)

				test.Eq(t, first.Key, policy.Key,
					test.Sprintf("%s tenants %s and %s got different limiter keys (%q vs %q), so "+
						"each tenant gets its own concurrency budget against one shared backend",
						platform, hosts[0], host, first.Key, policy.Key))
			}
		})
	}
}
