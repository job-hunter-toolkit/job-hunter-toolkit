package corpus

import (
	"net/url"
	"sort"
	"strings"
)

// trackingParams are the query parameters [NormalizeURL] drops.
//
// The allowlist is deliberately timid and deliberately explicit. Dropping an
// unknown parameter is how you merge two genuinely different postings into one
// row and lose one of them forever, and a corpus cannot recover from that by
// re-crawling. Every entry here is a parameter that provably carries no posting
// identity:
//
//   - gh_src and gh_jid are Greenhouse's own campaign and job-alert tags,
//     appended to links in outbound email; the same posting is reachable without
//     them.
//   - utm_* is the Urchin campaign convention every board's marketing stack
//     appends.
//   - source, ref and src are the generic referrer spellings.
//
// A parameter that is not on this list survives, including ones that look like
// noise, because "looks like noise" is not evidence.
var trackingParams = []string{
	"gh_src",
	"gh_jid",
	"source",
	"ref",
	"src",
}

// utmPrefix is matched as a prefix because the convention is open-ended:
// utm_source, utm_medium, utm_campaign, utm_term, utm_content and whatever a
// marketing team adds next.
const utmPrefix = "utm_"

// isTrackingParam reports whether a query parameter carries campaign
// attribution rather than posting identity.
func isTrackingParam(name string) bool {
	if strings.HasPrefix(name, utmPrefix) {
		return true
	}

	for _, candidate := range trackingParams {
		if name == candidate {
			return true
		}
	}

	return false
}

// NormalizeURL returns the form of raw used for the url identity basis: lowercase
// scheme and host, no fragment, no tracking parameters, remaining parameters
// sorted, no trailing slash.
//
// It is used *only* to derive identity, never to rewrite the URL a consumer is
// shown — [Row.Posting] keeps whatever the board published, because that is the
// link a person clicks and normalizing it is not this package's business.
//
// A URL that will not parse is returned unchanged. That is the conservative
// answer: an unparseable string is still a stable string, so it still identifies
// the posting it came from, and inventing a different key for it would split one
// posting into two rows on successive runs.
func NormalizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""

	if query := parsed.Query(); len(query) > 0 {
		names := make([]string, 0, len(query))

		for name := range query {
			if !isTrackingParam(name) {
				names = append(names, name)
			}
		}

		// Sorted because url.Values is a map, and map iteration order reaching a
		// hash would make identity depend on the run rather than on the posting.
		sort.Strings(names)

		var rebuilt strings.Builder
		for _, name := range names {
			values := query[name]

			// Repeated parameters keep their published order: ?a=1&a=2 and ?a=2&a=1
			// are different requests to a server that reads them positionally, so
			// they are different URLs here too.
			for _, value := range values {
				if rebuilt.Len() > 0 {
					rebuilt.WriteByte('&')
				}

				rebuilt.WriteString(url.QueryEscape(name))
				rebuilt.WriteByte('=')
				rebuilt.WriteString(url.QueryEscape(value))
			}
		}

		parsed.RawQuery = rebuilt.String()
	}

	// Only a trailing slash on a non-empty path: "https://example.com/" keeps its
	// root, because trimming it produces a URL with no path at all and the two
	// print differently.
	if len(parsed.Path) > 1 {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		parsed.RawPath = ""
	}

	return parsed.String()
}
