package shard

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal"
)

// PostingKeyBytes is the width of a posting fingerprint.
//
// 128 bits of SHA-256 is not a compression trick, it is what makes the merge
// fit. A full crawl is heading past 1.9 million postings and the URLs average
// ~80-120 bytes, so holding raw identities in the merge's dedupe set costs
// ~230 MB; sixteen-byte keys cost roughly a fifth of that. At 10^6 keys the
// collision probability at 128 bits is on the order of 10^-27.
const PostingKeyBytes = 16

// PostingRecord is one posting's entry in a shard's postings stream.
//
// It carries identity and nothing else. The merge needs exactly two numbers out
// of it — deduplicated postings and distinct companies — and a full posting
// stream would multiply every shard artifact by an order of magnitude for
// fields nothing downstream reads.
type PostingRecord struct {
	// Key is the hex-encoded fingerprint from [PostingKey].
	Key string `json:"key"`

	// Company is the posting's company, which is what the crawl's COMPANIES
	// column counts. It is the posting's own company rather than the source's,
	// because a source can publish for subsidiaries.
	Company string `json:"company"`

	// Platform is the ATS the posting came through, kept so a merge can report
	// per-platform coverage without a second file.
	Platform string `json:"platform,omitempty"`
}

// DedupeIdentity returns the string [internal.Dedupe] uses to decide whether
// two postings are the same posting.
//
// This must stay byte-identical to internal.Dedupe's key. A merge that
// deduplicates on a different identity than the in-process crawl does would
// produce a total that disagrees with `total` on the same data, and the
// disagreement would look like a coverage change rather than a bug.
// TestDedupeIdentityMatchesInternalDedupe pins the two together.
func DedupeIdentity(posting *internal.JobPosting) string {
	if posting == nil {
		return ""
	}

	if posting.URL != "" {
		return posting.URL
	}

	// Without a URL there is no stable identity, so fall back to the posting's
	// descriptive fields.
	return posting.Company + "\x00" + posting.Title + "\x00" + posting.Location
}

// PostingKey returns the hex-encoded fingerprint of a posting's identity.
func PostingKey(posting *internal.JobPosting) string {
	return hex.EncodeToString(fingerprint(DedupeIdentity(posting)))
}

func fingerprint(identity string) []byte {
	sum := sha256.Sum256([]byte(identity))

	return sum[:PostingKeyBytes]
}

// PostingWriter streams [PostingRecord] values as newline-delimited JSON.
//
// Streaming rather than buffering is deliberate: a shard that dies at its
// deadline still leaves every posting it had already counted on disk, and the
// merge cross-checks that line count against the manifest, so a truncated write
// is detected instead of silently shortening the total.
type PostingWriter struct {
	writer  *bufio.Writer
	encoder *json.Encoder
	written int
}

// NewPostingWriter returns a writer that emits NDJSON to w.
func NewPostingWriter(w io.Writer) *PostingWriter {
	buffered := bufio.NewWriterSize(w, 64*1024)

	return &PostingWriter{writer: buffered, encoder: json.NewEncoder(buffered)}
}

// Write emits one posting.
func (w *PostingWriter) Write(posting *internal.JobPosting) error {
	if posting == nil {
		return nil
	}

	record := PostingRecord{
		Key:      PostingKey(posting),
		Company:  posting.Company,
		Platform: posting.Source.Platform,
	}

	if err := w.encoder.Encode(record); err != nil {
		return fmt.Errorf("write posting record: %w", err)
	}

	w.written++

	return nil
}

// Written reports how many postings have been emitted.
func (w *PostingWriter) Written() int { return w.written }

// Flush pushes buffered records to the underlying writer.
func (w *PostingWriter) Flush() error {
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("flush postings stream: %w", err)
	}

	return nil
}

// readPostings streams the records in an NDJSON postings file, calling visit
// for each. It returns the number of records read.
//
// A malformed line is an error rather than something to skip: the whole point
// of this file is to be counted, and a parser that skips what it cannot read
// undercounts silently.
func readPostings(r io.Reader, name string, visit func(PostingRecord) error) (int, error) {
	scanner := bufio.NewScanner(r)

	// Postings carry long URLs and long company names; the default 64 KiB line
	// cap is generous but the failure if it is ever hit is a silent short read,
	// so raise it well past anything plausible.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	count := 0

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var record PostingRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return count, fmt.Errorf("decode postings %q line %d: %w", name, count+1, err)
		}

		if record.Key == "" {
			return count, fmt.Errorf("decode postings %q line %d: record has no key", name, count+1)
		}

		count++

		if err := visit(record); err != nil {
			return count, err
		}
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("read postings %q: %w", name, err)
	}

	return count, nil
}
