package shard

import (
	"context"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/httpx"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/services"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// noJobs stands in for an adapter. The planner never calls it; it exists so a
// synthetic source is shaped like a real one.
func noJobs(context.Context, *http.Client) internal.Jobs {
	return func(func(*internal.JobPosting, error) bool) {}
}

func source(platform, key string) services.Source {
	return services.Source{Platform: platform, Key: key, Company: key, Jobs: noJobs}
}

// syntheticSources mixes the two shapes the registry actually contains: a
// platform keyed by opaque board slugs, and a platform keyed by tenant
// hostnames.
func syntheticSources() []services.Source {
	return []services.Source{
		source("alpha", "a"),
		source("alpha", "b"),
		source("alpha", "c"),
		source("beta", "t1.example.com"),
		source("beta", "t2.example.com"),
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	t.Parallel()

	sources := syntheticSources()

	first, err := Build(sources, Options{ShardCount: 3})
	must.NoError(t, err)

	second, err := Build(slices.Clone(sources), Options{ShardCount: 3})
	must.NoError(t, err)

	must.Eq(t, first, second)
	test.NotEq(t, "", first.PlanID)
}

func TestBuildIgnoresSourceOrder(t *testing.T) {
	t.Parallel()

	// The registry hands back a platform round-robin whose order depends on how
	// many sources each platform has, so a plan that depended on input order
	// would silently reshuffle a crawl every time one company was added.
	sources := syntheticSources()

	want, err := Build(sources, Options{ShardCount: 2})
	must.NoError(t, err)

	shuffled := slices.Clone(sources)
	random := rand.New(rand.NewPCG(1, 2))
	random.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	got, err := Build(shuffled, Options{ShardCount: 2})
	must.NoError(t, err)

	must.Eq(t, want, got)
}

func TestBuildIsDeterministicOverTheRealRegistry(t *testing.T) {
	t.Parallel()

	sources := services.SourcesMatching(nil)
	must.SliceNotEmpty(t, sources)

	first, err := Build(sources, Options{ShardCount: 8})
	must.NoError(t, err)

	second, err := Build(sources, Options{ShardCount: 8})
	must.NoError(t, err)

	test.Eq(t, first.PlanID, second.PlanID)
	must.Eq(t, first, second)
	must.NoError(t, first.Validate())
	test.Eq(t, len(sources), first.SourceCount)
}

func TestBuildCoversEveryRegisteredSourceExactlyOnce(t *testing.T) {
	t.Parallel()

	sources := services.SourcesMatching(nil)

	plan, err := Build(sources, Options{ShardCount: 8})
	must.NoError(t, err)

	assigned := map[SourceRef]int{}
	for _, shard := range plan.Shards {
		for _, ref := range shard.Sources {
			assigned[ref.identity()]++
		}
	}

	must.Eq(t, len(sources), len(assigned))

	for _, source := range sources {
		ref := SourceRef{Platform: source.Platform, Key: source.Key}
		test.Eq(t, 1, assigned[ref], test.Sprintf("source %s", ref))
	}
}

// TestBuildNeverSplitsAnAffinityGroup is the invariant the whole package exists
// for: every shard is a separate process with a private httpx limiter, so a
// backend that appears in two shards is a backend seeing twice the concurrent
// requests it was measured to tolerate.
func TestBuildNeverSplitsAnAffinityGroup(t *testing.T) {
	t.Parallel()

	sources := services.SourcesMatching(nil)
	keys := AffinityKeys(sources, httpx.DefaultPerHostLimit)

	byRef := make(map[SourceRef]AffinityKey, len(sources))
	for i, source := range sources {
		byRef[SourceRef{Platform: source.Platform, Key: source.Key}] = keys[i]
	}

	for _, shardCount := range []int{1, 2, 3, 4, 8, 16, 64} {
		plan, err := Build(sources, Options{ShardCount: shardCount})
		must.NoError(t, err)

		home := map[AffinityKey]int{}

		for _, shard := range plan.Shards {
			for _, ref := range shard.Sources {
				key := byRef[ref.identity()]

				if previous, ok := home[key]; ok {
					must.Eq(t, previous, shard.Index,
						must.Sprintf("affinity key %s split across shards %d and %d at shardCount=%d",
							key, previous, shard.Index, shardCount))

					continue
				}

				home[key] = shard.Index
			}
		}

		// The shard's declared affinity keys must be exactly the keys of the
		// sources it holds, so the plan file is self-describing.
		for _, shard := range plan.Shards {
			declared := map[string]bool{}
			for _, key := range shard.AffinityKeys {
				declared[key] = true
			}

			for _, ref := range shard.Sources {
				test.True(t, declared[string(byRef[ref.identity()])],
					test.Sprintf("shard %d holds %s but does not declare its affinity key", shard.Index, ref))
			}
		}
	}
}

// TestSharedBackendPlatformsStayWhole names the platforms measured to sit
// behind a single host, so that a future change that lets one of them split
// fails here instead of in production.
func TestSharedBackendPlatformsStayWhole(t *testing.T) {
	t.Parallel()

	shared := []string{
		"ashby", "bamboohr", "gem", "greenhouse", "jibe", "jobvite", "lever",
		"peopleforce", "personio", "pinpoint", "recruitee", "rippling",
		"smartrecruiters", "teamtailor", "workable",
	}

	sources := services.SourcesMatching(nil)

	plan, err := Build(sources, Options{ShardCount: 16})
	must.NoError(t, err)

	home := map[string]int{}

	for _, shard := range plan.Shards {
		for _, ref := range shard.Sources {
			if !slices.Contains(shared, ref.Platform) {
				continue
			}

			if previous, ok := home[ref.Platform]; ok {
				must.Eq(t, previous, shard.Index,
					must.Sprintf("shared backend platform %q split across shards %d and %d",
						ref.Platform, previous, shard.Index))

				continue
			}

			home[ref.Platform] = shard.Index
		}
	}
}

// TestSplittablePlatformsAreReviewed keeps the set of platforms the planner is
// willing to spread over runners under human control. Adding a slug-keyed
// platform passes; adding one whose keys are tenant hostnames does not, and
// that is the moment someone has to confirm those hosts really are independent.
func TestSplittablePlatformsAreReviewed(t *testing.T) {
	t.Parallel()

	reviewed := []string{
		// 216 separate tenant hosts, each its own limiter key in httpx.
		"workday",
		// 15 separate career-site hostnames.
		"phenom",
		// Tenant pods; httpx keys on the exact pod host, so tenants sharing a
		// pod stay together and different pods may separate.
		"oraclecloud",
		"successfactors",
	}

	splittable := SplittablePlatforms(services.SourcesMatching(nil), httpx.DefaultPerHostLimit)

	for _, platform := range splittable {
		test.True(t, slices.Contains(reviewed, platform), test.Sprintf(
			"platform %q became splittable across shards without review; confirm its tenants really are on independent hosts, then add it to this list",
			platform))
	}
}

func TestAffinityKeysUseHTTPXGroupingForSharedTenantSubdomains(t *testing.T) {
	t.Parallel()

	// httpx collapses every *.peopleforce.io tenant onto one limiter key. The
	// planner must inherit that rather than deciding for itself that separate
	// subdomains are separate backends.
	sources := []services.Source{
		source("smb", "alpha.peopleforce.io"),
		source("smb", "beta.peopleforce.io"),
	}

	keys := AffinityKeys(sources, httpx.DefaultPerHostLimit)

	must.Eq(t, keys[0], keys[1])
	test.Eq(t, AffinityKey("service:peopleforce.io"), keys[0])
	test.False(t, keys[0].Platform())
}

func TestAffinityKeysSeparateIsolatedTenantHosts(t *testing.T) {
	t.Parallel()

	sources := []services.Source{
		source("workday", "https://3m.wd1.myworkdayjobs.com/Search"),
		source("workday", "https://lilly.wd115.myworkdayjobs.com/LLY"),
	}

	keys := AffinityKeys(sources, httpx.DefaultPerHostLimit)

	test.NotEq(t, keys[0], keys[1])
	test.Eq(t, AffinityKey("service:3m.wd1.myworkdayjobs.com"), keys[0])
}

func TestAffinityKeysFallBackToPlatformWhenAnyKeyIsOpaque(t *testing.T) {
	t.Parallel()

	// One slug among hostnames means the platform's topology is not knowable
	// offline, so the whole platform stays together. Over-grouping costs
	// parallelism; under-grouping doubles a backend's request rate.
	sources := []services.Source{
		source("mixed", "tenant-one.example.com"),
		source("mixed", "opaque-slug"),
	}

	keys := AffinityKeys(sources, httpx.DefaultPerHostLimit)

	must.Eq(t, keys[0], keys[1])
	test.Eq(t, AffinityKey("platform:mixed"), keys[0])
	test.True(t, keys[0].Platform())
}

func TestAffinityKeysExtractHostsFromCompositeKeys(t *testing.T) {
	t.Parallel()

	sources := []services.Source{
		source("oraclecloud", "kroger,eluq.fa.us2.oraclecloud.com,CX_2001"),
		source("oraclecloud", "other,eluq.fa.us2.oraclecloud.com,CX_1"),
		source("oraclecloud", "brookdale,ibmwjb.fa.ocs.oraclecloud.com,CX_1"),
	}

	keys := AffinityKeys(sources, httpx.DefaultPerHostLimit)

	// Two tenants on one pod share a limiter, a third pod does not.
	must.Eq(t, keys[0], keys[1])
	test.NotEq(t, keys[0], keys[2])
	test.Eq(t, AffinityKey("service:eluq.fa.us2.oraclecloud.com"), keys[0])
}

func TestLooksLikeHostnameRejectsBoardSlugs(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{"", "2k", "0x", "lumalabs-ai", "CX_2001", "CRH", "KimberlyClark", "x.y", "1.2.3.4", "a/b.com", "host:8080"} {
		test.False(t, looksLikeHostname(slug), test.Sprintf("slug %q was mistaken for a hostname", slug))
	}

	for _, host := range []string{"careers.humana.com", "career2.successfactors.eu", "3m.wd1.myworkdayjobs.com"} {
		test.True(t, looksLikeHostname(host), test.Sprintf("hostname %q was not recognised", host))
	}
}

func TestBuildRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	_, err := Build(syntheticSources(), Options{ShardCount: 0})
	must.ErrorContains(t, err, "at least one shard")

	_, err = Build(nil, Options{ShardCount: 2})
	must.ErrorContains(t, err, "no sources to plan")

	duplicated := []services.Source{source("alpha", "a"), source("alpha", "a")}
	_, err = Build(duplicated, Options{ShardCount: 2})
	must.ErrorContains(t, err, "registered more than once")
}

func TestPlanValidateRejectsTampering(t *testing.T) {
	t.Parallel()

	base, err := Build(syntheticSources(), Options{ShardCount: 2})
	must.NoError(t, err)
	must.NoError(t, base.Validate())

	t.Run("schema version", func(t *testing.T) {
		plan := clonePlan(base)
		plan.SchemaVersion = PlanSchemaVersion + 1
		must.ErrorContains(t, plan.Validate(), "schema version")
	})

	t.Run("shard count", func(t *testing.T) {
		plan := clonePlan(base)
		plan.ShardCount = 3
		must.ErrorContains(t, plan.Validate(), "declares 3 shards")
	})

	t.Run("source assigned twice", func(t *testing.T) {
		plan := clonePlan(base)
		plan.Shards[1].Sources = append(plan.Shards[1].Sources, plan.Shards[0].Sources[0])
		plan.SourceCount++
		must.ErrorContains(t, plan.Validate(), "doubles that backend's request rate")
	})

	t.Run("source count", func(t *testing.T) {
		plan := clonePlan(base)
		plan.SourceCount = 99
		must.ErrorContains(t, plan.Validate(), "assigns")
	})

	t.Run("edited assignment", func(t *testing.T) {
		plan := clonePlan(base)
		plan.Shards[0].Sources = plan.Shards[0].Sources[:len(plan.Shards[0].Sources)-1]
		plan.SourceCount--
		must.ErrorContains(t, plan.Validate(), "edited after it was built")
	})
}

func TestResolveFailsClosedOnUnknownSource(t *testing.T) {
	t.Parallel()

	sources := syntheticSources()

	plan, err := Build(sources, Options{ShardCount: 1})
	must.NoError(t, err)

	resolved, err := plan.Resolve(0, sources)
	must.NoError(t, err)
	test.Eq(t, len(sources), len(resolved))

	// A binary that no longer registers one of the planned sources must refuse
	// to crawl the shard rather than report a clean manifest for a subset.
	stale := slices.Delete(slices.Clone(sources), 0, 1)
	_, err = plan.Resolve(0, stale)
	must.ErrorContains(t, err, "not registered in this binary")
	must.ErrorContains(t, err, "alpha/a")

	_, err = plan.Resolve(7, sources)
	must.ErrorContains(t, err, "outside the plan's")
}

func TestResolvePreservesRegistryOrder(t *testing.T) {
	t.Parallel()

	// The registry's interleaved order is what keeps a crawl's opening wave
	// spread over backends; the shard must not re-sort it by platform.
	sources := []services.Source{
		source("beta", "t1.example.com"),
		source("alpha", "a"),
		source("beta", "t2.example.com"),
		source("alpha", "b"),
	}

	plan, err := Build(sources, Options{ShardCount: 1})
	must.NoError(t, err)

	resolved, err := plan.Resolve(0, sources)
	must.NoError(t, err)

	got := make([]string, 0, len(resolved))
	for _, resolvedSource := range resolved {
		got = append(got, resolvedSource.Platform+"/"+resolvedSource.Key)
	}

	must.Eq(t, []string{"beta/t1.example.com", "alpha/a", "beta/t2.example.com", "alpha/b"}, got)
}

func TestSourceSetIDTracksIdentityNotDisplayNames(t *testing.T) {
	t.Parallel()

	sources := syntheticSources()
	base := SourceSetID(sources)

	renamed := slices.Clone(sources)
	renamed[0].Company = "A Different Display Name"
	test.Eq(t, base, SourceSetID(renamed))

	// Order must not matter: the registry order changes whenever a platform
	// gains a company.
	reordered := slices.Clone(sources)
	slices.Reverse(reordered)
	test.Eq(t, base, SourceSetID(reordered))

	added := append(slices.Clone(sources), source("alpha", "d"))
	test.NotEq(t, base, SourceSetID(added))
}

func TestMatrixIndexes(t *testing.T) {
	t.Parallel()

	plan, err := Build(syntheticSources(), Options{ShardCount: 4})
	must.NoError(t, err)

	must.Eq(t, []int{0, 1, 2, 3}, plan.MatrixIndexes())
}

func TestPlanRoundTripsThroughDisk(t *testing.T) {
	t.Parallel()

	want, err := Build(services.SourcesMatching(nil), Options{ShardCount: 6, Commit: "abc123"})
	must.NoError(t, err)

	path := t.TempDir() + "/plan.json"
	must.NoError(t, WritePlan(path, want))

	got, err := ReadPlan(path)
	must.NoError(t, err)

	test.Eq(t, want.PlanID, got.PlanID)
	test.Eq(t, want.SourceSetID, got.SourceSetID)
	test.Eq(t, want.Commit, got.Commit)
	must.Eq(t, want.Shards, got.Shards)
}

func TestDecodePlanRejectsAnEditedFile(t *testing.T) {
	t.Parallel()

	plan, err := Build(syntheticSources(), Options{ShardCount: 2})
	must.NoError(t, err)

	plan.Shards[0].Sources = nil

	path := t.TempDir() + "/plan.json"
	must.NoError(t, WritePlan(path, plan))

	_, err = ReadPlan(path)
	must.ErrorContains(t, err, "not usable")
}

func clonePlan(plan Plan) Plan {
	cloned := plan
	cloned.Shards = slices.Clone(plan.Shards)

	for i := range cloned.Shards {
		cloned.Shards[i].Sources = slices.Clone(plan.Shards[i].Sources)
		cloned.Shards[i].AffinityKeys = slices.Clone(plan.Shards[i].AffinityKeys)
	}

	return cloned
}

func TestPlanSummaryNamesTheLargestGroup(t *testing.T) {
	t.Parallel()

	// Not an assertion about a specific number of sources, which changes as
	// companies are added: only that the biggest indivisible unit is a shared
	// backend and therefore bounds the crawl no matter how many shards run.
	sources := services.SourcesMatching(nil)
	keys := AffinityKeys(sources, httpx.DefaultPerHostLimit)

	sizes := map[AffinityKey]int{}
	for _, key := range keys {
		sizes[key]++
	}

	largest := AffinityKey("")
	for key, size := range sizes {
		if size > sizes[largest] {
			largest = key
		}
	}

	test.True(t, strings.HasPrefix(string(largest), platformPrefix),
		test.Sprintf("largest affinity group %q is unexpectedly not a whole platform", largest))
}
