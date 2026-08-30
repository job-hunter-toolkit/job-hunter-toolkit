// Package bootstrap builds and verifies the small, additive default-page
// projection used for useful paint while the complete browser engine loads.
package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/internal/corpus"
	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/engine"
)

const (
	Schema           = "jht-default-page-bootstrap"
	Version          = 1
	ReaderVersion    = 1
	DefaultPageLimit = 100
	MaxBytes         = 256 << 10
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Document is independently digest-verifiable without opening corpus.jhtc.
// PayloadDigest covers the canonical JSON encoding of Payload. ContentDigest
// binds that payload to the foreground manifest and every RowBinding binds one
// card to its exact table row and page position within that generation.
type Document struct {
	Schema           string  `json:"schema"`
	Version          int     `json:"version"`
	MinReaderVersion int     `json:"min_reader_version"`
	PayloadDigest    string  `json:"payload_digest"`
	Payload          Payload `json:"payload"`
}

type Payload struct {
	Generation      int64          `json:"generation"`
	ContentDigest   string         `json:"content_digest"`
	FormatVersion   int            `json:"format_version"`
	IdentityVersion int            `json:"identity_version"`
	RunAt           string         `json:"run_at"`
	EvaluatedAt     string         `json:"evaluated_at"`
	Partial         bool           `json:"partial"`
	Rows            int            `json:"rows"`
	Request         DefaultRequest `json:"request"`
	Response        Response       `json:"response"`
}

type DefaultRequest struct {
	Kind  string   `json:"kind"`
	Sort  string   `json:"sort"`
	State []string `json:"state"`
	Limit int      `json:"limit"`
}

type Response struct {
	Matched   int    `json:"matched"`
	CountUnit string `json:"count_unit"`
	States    Counts `json:"states"`
	Items     []Item `json:"items"`
}

type Counts struct {
	Open   int `json:"open"`
	Stale  int `json:"stale"`
	Closed int `json:"closed"`
	Lapsed int `json:"lapsed"`
}

type Item struct {
	Position   int         `json:"position"`
	SourceRow  uint32      `json:"source_row"`
	RowBinding string      `json:"row_binding"`
	Record     engine.Item `json:"record"`
}

// Expected is authoritative foreground metadata. Zero-valued fields are not
// checked, which lets tests and future readers validate only facts they have
// already obtained without weakening checks in the browser integration.
type Expected struct {
	Generation      int64
	ContentDigest   string
	FormatVersion   int
	IdentityVersion int
	Partial         *bool
	Rows            int
}

// Generate loads one generation and projects its deterministic default page.
// at is explicit because lifecycle labels are clock-derived. A zero value pins
// evaluation to the immutable generation run time, producing reproducible bytes.
func Generate(ctx context.Context, store corpus.Store, at time.Time) (Document, error) {
	opened, err := engine.Open(ctx, store, at)
	if err != nil {
		return Document{}, err
	}
	summary := opened.Summary()
	if at.IsZero() {
		at, err = time.Parse(time.RFC3339, summary.RunAt)
		if err != nil {
			return Document{}, fmt.Errorf("bootstrap: generation has no valid run_at: %w", err)
		}
		opened, err = engine.Open(ctx, store, at)
		if err != nil {
			return Document{}, err
		}
		summary = opened.Summary()
	}
	if err := opened.Load(ctx); err != nil {
		return Document{}, err
	}

	page, err := opened.DefaultBootstrapPage(DefaultPageLimit)
	if err != nil {
		return Document{}, err
	}
	payload := Payload{
		Generation:      summary.Generation,
		ContentDigest:   summary.ContentDigest,
		FormatVersion:   summary.FormatVersion,
		IdentityVersion: summary.IdentityVersion,
		RunAt:           summary.RunAt,
		EvaluatedAt:     at.UTC().Format(time.RFC3339),
		Partial:         summary.Partial,
		Rows:            summary.Rows,
		Request: DefaultRequest{
			Kind: "verified_default_page", Sort: "effective-posted-first-v1",
			State: []string{"open", "stale"}, Limit: DefaultPageLimit,
		},
		Response: Response{
			Matched: page.Matched, CountUnit: page.CountUnit,
			States: Counts{
				Open: page.States["open"], Stale: page.States["stale"],
				Closed: page.States["closed"], Lapsed: page.States["lapsed"],
			},
			Items: make([]Item, len(page.Items)),
		},
	}
	for i, pageItem := range page.Items {
		payload.Response.Items[i] = Item{
			Position: i, SourceRow: pageItem.SourceRow, Record: pageItem.Item,
		}
		payload.Response.Items[i].RowBinding = rowBinding(payload.ContentDigest, payload.Response.Items[i])
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return Document{}, err
	}
	document := Document{
		Schema: Schema, Version: Version, MinReaderVersion: ReaderVersion,
		PayloadDigest: digest(payloadBytes), Payload: payload,
	}
	encoded, err := Marshal(document)
	if err != nil {
		return Document{}, err
	}
	if len(encoded) > MaxBytes {
		return Document{}, fmt.Errorf("bootstrap: %d bytes exceeds %d-byte limit", len(encoded), MaxBytes)
	}

	return document, nil
}

func Marshal(document Document) ([]byte, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// Parse verifies shape, bounds, independent digests, row bindings, and any
// authoritative foreground metadata before returning a usable document.
func Parse(data []byte, expected Expected) (Document, error) {
	if len(data) > MaxBytes {
		return Document{}, fmt.Errorf("bootstrap: %d bytes exceeds %d-byte limit", len(data), MaxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var wire struct {
		Schema           string          `json:"schema"`
		Version          int             `json:"version"`
		MinReaderVersion int             `json:"min_reader_version"`
		PayloadDigest    string          `json:"payload_digest"`
		Payload          json.RawMessage `json:"payload"`
	}
	if err := decoder.Decode(&wire); err != nil {
		return Document{}, fmt.Errorf("bootstrap: decode: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Document{}, errors.New("bootstrap: trailing JSON value")
	}
	document := Document{
		Schema: wire.Schema, Version: wire.Version, MinReaderVersion: wire.MinReaderVersion,
		PayloadDigest: wire.PayloadDigest,
	}
	if !sha256Pattern.MatchString(document.PayloadDigest) || digest(wire.Payload) != document.PayloadDigest {
		return Document{}, errors.New("bootstrap: payload digest mismatch")
	}
	if err := json.Unmarshal(wire.Payload, &document.Payload); err != nil {
		return Document{}, fmt.Errorf("bootstrap: decode payload: %w", err)
	}
	if err := validate(document, expected); err != nil {
		return Document{}, err
	}
	return document, nil
}

func validate(document Document, expected Expected) error {
	if document.Schema != Schema {
		return fmt.Errorf("bootstrap: unknown schema %q", document.Schema)
	}
	if document.MinReaderVersion > ReaderVersion {
		return fmt.Errorf("bootstrap: reader v%d is older than required v%d", ReaderVersion, document.MinReaderVersion)
	}
	if document.Version != Version {
		return fmt.Errorf("bootstrap: unsupported version %d", document.Version)
	}
	if !sha256Pattern.MatchString(document.Payload.ContentDigest) {
		return errors.New("bootstrap: invalid digest encoding")
	}
	p := document.Payload
	if p.Generation <= 0 || p.Rows <= 0 || p.FormatVersion <= 0 || p.IdentityVersion <= 0 {
		return errors.New("bootstrap: invalid generation metadata")
	}
	if _, err := time.Parse(time.RFC3339, p.RunAt); err != nil {
		return errors.New("bootstrap: invalid run_at")
	}
	if _, err := time.Parse(time.RFC3339, p.EvaluatedAt); err != nil {
		return errors.New("bootstrap: invalid evaluated_at")
	}
	if p.Request.Kind != "verified_default_page" || p.Request.Sort != "effective-posted-first-v1" ||
		len(p.Request.State) != 2 || p.Request.State[0] != "open" || p.Request.State[1] != "stale" ||
		p.Request.Limit != DefaultPageLimit {
		return errors.New("bootstrap: unsupported default-page request")
	}
	if p.Response.CountUnit != "rows" || p.Response.Matched < len(p.Response.Items) || len(p.Response.Items) > DefaultPageLimit {
		return errors.New("bootstrap: invalid response bounds")
	}
	wantItems := min(p.Response.Matched, DefaultPageLimit)
	if len(p.Response.Items) != wantItems {
		return errors.New("bootstrap: partial default page")
	}
	if p.Response.States.Open+p.Response.States.Stale+p.Response.States.Closed+p.Response.States.Lapsed != p.Response.Matched {
		return errors.New("bootstrap: state counts do not equal matched rows")
	}
	if p.Response.States.Closed != 0 || p.Response.States.Lapsed != 0 {
		return errors.New("bootstrap: default page includes excluded lifecycle states")
	}
	seenRows := make(map[uint32]struct{}, len(p.Response.Items))
	for i, item := range p.Response.Items {
		if item.Position != i || int(item.SourceRow) >= p.Rows {
			return errors.New("bootstrap: invalid item position or source row")
		}
		if item.Record.State != "open" && item.Record.State != "stale" {
			return errors.New("bootstrap: item has excluded lifecycle state")
		}
		if _, duplicate := seenRows[item.SourceRow]; duplicate {
			return errors.New("bootstrap: duplicate source row")
		}
		seenRows[item.SourceRow] = struct{}{}
		if item.RowBinding != rowBinding(p.ContentDigest, item) {
			return fmt.Errorf("bootstrap: row binding mismatch at position %d", i)
		}
	}
	if expected.Generation != 0 && p.Generation != expected.Generation {
		return errors.New("bootstrap: generation mismatch")
	}
	if expected.ContentDigest != "" && p.ContentDigest != expected.ContentDigest {
		return errors.New("bootstrap: content digest mismatch")
	}
	if expected.FormatVersion != 0 && p.FormatVersion != expected.FormatVersion {
		return errors.New("bootstrap: format version mismatch")
	}
	if expected.IdentityVersion != 0 && p.IdentityVersion != expected.IdentityVersion {
		return errors.New("bootstrap: identity version mismatch")
	}
	if expected.Partial != nil && p.Partial != *expected.Partial {
		return errors.New("bootstrap: partial status mismatch")
	}
	if expected.Rows != 0 && p.Rows != expected.Rows {
		return errors.New("bootstrap: row count mismatch")
	}
	return nil
}

func rowBinding(contentDigest string, item Item) string {
	record, _ := json.Marshal(item.Record)
	value := fmt.Sprintf("jht-bootstrap-row-v1\n%s\n%d\n%d\n", contentDigest, item.Position, item.SourceRow)
	hash := sha256.New()
	_, _ = io.WriteString(hash, value)
	_, _ = hash.Write(record)
	return hex.EncodeToString(hash.Sum(nil))
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// WriteFile atomically promotes a fully validated document. Any failure before
// rename, including a quota error or short write, leaves an existing file untouched.
func WriteFile(path string, document Document) error {
	encoded, err := Marshal(document)
	if err != nil {
		return err
	}
	if _, err := Parse(encoded, Expected{}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".bootstrap-staging-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err = temporary.Write(encoded); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
