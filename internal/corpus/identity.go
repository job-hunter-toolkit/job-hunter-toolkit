package corpus

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/jobposting"
)

// IdentityVersion is bumped only when ID derivation changes, and bumping it
// forces a rebuild rather than a migration: every row is renumbered, so a
// compaction cannot do it. A rebuild must emit an (old_id, new_id) mapping so
// first_seen and closure history carry over — without one, an identity change
// silently resets every date in a format whose only purpose is dates.
const IdentityVersion = 1

// idDomain separates this hash from every other SHA-256 in the project. Domain
// separation costs sixteen bytes per hash and makes it impossible for a
// [DedupeKey] and an [Identify] result to ever be the same sixteen bytes for the
// same input.
const idDomain = "jht-corpus-id-v1"

// IDBytes is the width of a corpus id, matching shard.PostingKeyBytes. At 10^6
// rows the collision probability at 128 bits is on the order of 10^-27.
const IDBytes = 16

// Basis records which rung of the identity ladder produced a row's ID.
//
// It is stored on the row, not derived, because a consumer has to be able to
// distrust the bottom rung: a descriptor identity is content-derived, so an
// employer who fixes a typo in a title ends one row and starts another. Every
// other basis survives an edit.
type Basis string

// The identity ladder, highest first. See docs/design/corpus-format.md §1.2.
const (
	// BasisExternal is the ATS's own posting id. Present on 17 of the 19
	// platforms sampled, at 100% coverage within each, and absent on jibe and
	// workday — which contributed 377,855 of the 07/28 crawl's 840,732 raw
	// postings, so 45% of the corpus cannot reach this rung today.
	BasisExternal Basis = "external"

	// BasisRequisition is the employer's own requisition number. It is the field a
	// human wants in a referral email and the field that must never be identity by
	// default: greenhouse/stripe publishes the literal string "See Opening ID" on
	// 531 of 532 postings, and greenhouse/databricks reuses 86 ids across 800
	// postings. It is used only where it is provably unique within the source, and
	// a source that has ever collided is marked [SourceState.RequisitionUnsafe]
	// and never promoted again.
	BasisRequisition Basis = "requisition"

	// BasisURL is the normalized URL. Stable on most platforms and not on all:
	// Teamtailor embeds the title in the path and Recruitee's is a title slug with
	// no id at all, so an edited title is a new URL and a URL-keyed identity would
	// report a close and a fresh opening for a typo fix.
	BasisURL Basis = "url"

	// BasisDescriptor is sha256(title ‖ 0x00 ‖ location), the last resort, reached
	// only when a board publishes neither an id nor a URL.
	BasisDescriptor Basis = "descriptor"
)

// ladder is the ordered list [Identify] walks. Order is the definition of the
// rule, so it lives in one place.
var ladder = []Basis{BasisExternal, BasisRequisition, BasisURL, BasisDescriptor}

// ID derives a posting's corpus id from its integration and one ladder rung.
//
//	ID = sha256(domain ‖ 0 ‖ platform ‖ 0 ‖ key ‖ 0 ‖ basis ‖ 0 ‖ value)[:16]
//
// Identity is scoped to the integration and never global. That is what makes
// per-source closure sound: a row can only ever be closed by evidence from the
// one source that produced it, so a Greenhouse failure cannot reason about an
// Ashby row. It also means an employer on two ATSs has two rows, which is the
// truth — two applications, two URLs, two closing dates.
func ID(source jobposting.PostingSource, basis Basis, value string) string {
	h := sha256.New()

	for _, part := range []string{idDomain, source.Platform, source.Key, string(basis), value} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}

	sum := h.Sum(nil)

	return hex.EncodeToString(sum[:IDBytes])
}

// DedupeKey is byte-identical to shard.PostingKey: SHA-256 of the identity
// internal.Dedupe uses, truncated to sixteen bytes.
//
// It is carried on every row so the corpus's headline count is the count of
// distinct dedupe keys among open rows — exactly the global union `shard merge`
// computes today, unchanged. Continuity of that column in jobs_record.txt is a
// hard requirement, and this is the field that delivers it. It is not identity:
// identity is [ID].
//
// Duplicated here rather than imported because internal/shard pulls in the
// crawler; the shape is four lines and TestDedupeKeyMatchesShardPostingKey pins
// the two together.
func DedupeKey(posting *jobposting.JobPosting) string {
	if posting == nil {
		return ""
	}

	identity := posting.URL
	if identity == "" {
		identity = posting.Company + "\x00" + posting.Title + "\x00" + posting.Location
	}

	sum := sha256.Sum256([]byte(identity))

	return hex.EncodeToString(sum[:IDBytes])
}

// descriptorValue is the bottom rung's value: a hash rather than the raw text,
// so a title carrying a NUL or a megabyte of whitespace cannot bloat the key.
func descriptorValue(posting *jobposting.JobPosting) string {
	sum := sha256.Sum256([]byte(posting.Title + "\x00" + posting.Location))

	return hex.EncodeToString(sum[:])
}

// candidate returns the value a posting offers for one rung, empty when it
// offers none.
func candidate(posting *jobposting.JobPosting, basis Basis) string {
	switch basis {
	case BasisExternal:
		return posting.ExternalID
	case BasisRequisition:
		return posting.RequisitionID
	case BasisURL:
		return NormalizeURL(posting.URL)
	case BasisDescriptor:
		return descriptorValue(posting)
	default:
		return ""
	}
}

// Identities is the outcome of resolving one source's postings in one run.
type Identities struct {
	// IDs and Bases are parallel to the postings passed to [Identify].
	IDs   []string
	Bases []Basis

	// Demoted lists the rungs this run found colliding within the source, highest
	// first. A [BasisRequisition] entry is what sets [SourceState.RequisitionUnsafe]
	// permanently: a lucky day must not re-promote a field that collided
	// yesterday.
	Demoted []Basis

	// Collisions counts postings dropped because two of them resolved to the same
	// id even at the bottom rung — two postings at one source with the same title
	// and location and no id or URL to tell them apart. The corpus cannot
	// represent them as two rows, so the first wins and this reports how often
	// that happened rather than hiding it.
	Collisions int
}

// Identify assigns corpus ids to one source's postings from one run.
//
// The ladder is resolved per *source*, not per posting: a rung is used only if
// every posting that offers a value for it offers a distinct one. Stripe's "See
// Opening ID" collides 531 ways, so requisition is demoted for that source and
// external — which Greenhouse does publish — is used instead; had Greenhouse
// published nothing, it would fall to url. This is one pass over a source's rows,
// deterministic, and needs no per-platform table, for the same reason
// internal/shard derives affinity from httpx's policy table rather than curating
// a second one beside it.
//
// requisitionUnsafe is the source's sticky memory of a past collision, from
// [SourceState.RequisitionUnsafe]. Pass it and honour the result.
//
// The returned slices are parallel to postings. A nil posting yields an empty id
// and is skipped; callers must check for the empty id rather than assuming every
// input produced a row.
func Identify(source jobposting.PostingSource, postings []*jobposting.JobPosting, requisitionUnsafe bool) Identities {
	out := Identities{
		IDs:   make([]string, len(postings)),
		Bases: make([]Basis, len(postings)),
	}

	usable := map[Basis]bool{}

	for _, basis := range ladder {
		if basis == BasisDescriptor {
			// The bottom rung is never demoted, because there is nothing below it
			// to demote to. Demoting it source-wide would drop every descriptor-only
			// posting at a source where any two of them happened to share a title and
			// a location, which is far more destructive than the ambiguity it avoids.
			// Genuine duplicates are caught per posting below and counted.
			usable[basis] = true

			continue
		}

		if basis == BasisRequisition && requisitionUnsafe {
			out.Demoted = append(out.Demoted, basis)

			continue
		}

		seen := make(map[string]struct{}, len(postings))
		collides := false

		for _, posting := range postings {
			if posting == nil {
				continue
			}

			value := candidate(posting, basis)
			if value == "" {
				continue
			}

			if _, duplicate := seen[value]; duplicate {
				collides = true

				break
			}

			seen[value] = struct{}{}
		}

		if collides {
			// Demotion is scoped to the run that observed the collision for every
			// rung except requisition, whose demotion the caller makes permanent.
			// A URL that collided once because a board double-published is not a
			// reason to abandon URL identity forever.
			out.Demoted = append(out.Demoted, basis)

			continue
		}

		usable[basis] = true
	}

	assigned := make(map[string]struct{}, len(postings))

	for i, posting := range postings {
		if posting == nil {
			continue
		}

		for _, basis := range ladder {
			if !usable[basis] {
				continue
			}

			value := candidate(posting, basis)
			if value == "" {
				continue
			}

			out.IDs[i] = ID(source, basis, value)
			out.Bases[i] = basis

			break
		}

		if _, duplicate := assigned[out.IDs[i]]; out.IDs[i] == "" || duplicate {
			out.IDs[i] = ""
			out.Bases[i] = ""
			out.Collisions++

			continue
		}

		assigned[out.IDs[i]] = struct{}{}
	}

	return out
}
