package corpus

import (
	"testing"

	"github.com/shoenig/test"
)

func TestNormalizeURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"unchanged", "https://job-boards.greenhouse.io/anthropic/jobs/1", "https://job-boards.greenhouse.io/anthropic/jobs/1"},
		{"lowercases scheme and host", "HTTPS://Job-Boards.Greenhouse.IO/anthropic/jobs/1", "https://job-boards.greenhouse.io/anthropic/jobs/1"},
		{"keeps path case", "https://example.com/Jobs/Senior-Engineer", "https://example.com/Jobs/Senior-Engineer"},
		{"drops the fragment", "https://example.com/jobs/1#apply", "https://example.com/jobs/1"},
		{"strips a trailing slash", "https://example.com/jobs/1/", "https://example.com/jobs/1"},
		{"keeps the root slash", "https://example.com/", "https://example.com/"},
		{"sorts query parameters", "https://example.com/j?b=2&a=1", "https://example.com/j?a=1&b=2"},
		{"keeps unknown parameters", "https://example.com/j?mystery=1", "https://example.com/j?mystery=1"},
		{"keeps repeated parameters in order", "https://example.com/j?a=2&a=1", "https://example.com/j?a=2&a=1"},

		// One case per allowlist entry, because an allowlist with an untested entry
		// is an allowlist nobody can safely add to.
		{"drops gh_src", "https://example.com/j?gh_src=abc", "https://example.com/j"},
		{"drops gh_jid", "https://example.com/j?gh_jid=123", "https://example.com/j"},
		{"drops source", "https://example.com/j?source=linkedin", "https://example.com/j"},
		{"drops ref", "https://example.com/j?ref=twitter", "https://example.com/j"},
		{"drops src", "https://example.com/j?src=email", "https://example.com/j"},
		{"drops utm_source", "https://example.com/j?utm_source=x", "https://example.com/j"},
		{"drops utm_medium", "https://example.com/j?utm_medium=x", "https://example.com/j"},
		{"drops utm_campaign", "https://example.com/j?utm_campaign=x", "https://example.com/j"},
		{"drops utm_term", "https://example.com/j?utm_term=x", "https://example.com/j"},
		{"drops utm_content", "https://example.com/j?utm_content=x", "https://example.com/j"},
		{"drops a future utm parameter", "https://example.com/j?utm_whatever=x", "https://example.com/j"},

		{"keeps the id beside the tracking", "https://example.com/j?gh_jid=1&id=99", "https://example.com/j?id=99"},

		// "sourceId" is not "source". A prefix match here would merge two postings.
		{"keeps sourceId", "https://example.com/j?sourceId=7", "https://example.com/j?sourceId=7"},
		{"keeps reference", "https://example.com/j?reference=7", "https://example.com/j?reference=7"},

		// An unparseable string is still a stable string, so it still identifies the
		// posting it came from. Inventing a different key for it would split one
		// posting into two rows on successive runs.
		{"passes through what will not parse", "://not a url", "://not a url"},
		{"trims surrounding space", "  https://example.com/j  ", "https://example.com/j"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, c.want, NormalizeURL(c.in))
		})
	}
}

func TestNormalizeURLIsIdempotent(t *testing.T) {
	t.Parallel()

	// Identity is derived from this, so applying it twice has to be a no-op:
	// otherwise a normalized URL fed back through would produce a different row.
	for _, raw := range []string{
		"HTTPS://Example.COM/Jobs/1/?utm_source=x&b=2&a=1#apply",
		"https://example.com/",
		"://not a url",
		"",
	} {
		once := NormalizeURL(raw)
		test.Eq(t, once, NormalizeURL(once), test.Sprintf("input %q", raw))
	}
}
