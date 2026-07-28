package services

import "hash/fnv"

// pageRepeatGuard recognises a board that ignores its page or offset parameter.
//
// Every offset-based adapter in this package used to decide it was finished only
// when a page came back short or empty. A tenant that answers every offset with
// the same full first page therefore never ends the loop: replayed against a
// stub transport that behaves that way, Lever, Jibe and Phenom each issued 5,001
// requests and yielded 500,001 duplicate postings in under a second, and stopped
// only because the consumer gave up. In a real crawl the loop ends at the crawl
// deadline instead, pinning a worker slot and hammering a single host for hours,
// while [internal.Dedupe] hides the duplicates so the posting total looks
// unremarkable.
//
// The guard fingerprints each page rather than retaining every posting URL: a
// per-source URL set costs roughly 124 bytes per posting and the largest tenants
// publish six figures of them, whereas a page fingerprint is 8 bytes per page.
//
// It lives here rather than beside any one adapter because five of them share
// it; it was originally written in lever.go only because the adapters were being
// repaired concurrently and that file happened to be the one holding it.
//
// The zero value is ready to use.
type pageRepeatGuard struct {
	seen map[uint64]struct{}
}

// repeated reports whether a page whose postings are identified by ids has
// already been returned for this source, which means the board is not honouring
// pagination and the caller must stop rather than ask for the next page.
func (g *pageRepeatGuard) repeated(ids []string) bool {
	sum := fnv.New64a()

	for _, id := range ids {
		// The separator keeps {"ab", "c"} from fingerprinting the same as
		// {"a", "bc"}.
		_, _ = sum.Write([]byte(id))
		_, _ = sum.Write([]byte{0})
	}

	key := sum.Sum64()

	if _, ok := g.seen[key]; ok {
		return true
	}

	if g.seen == nil {
		g.seen = make(map[uint64]struct{})
	}

	g.seen[key] = struct{}{}

	return false
}
