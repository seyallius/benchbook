// Package model. encode - canonical JSON encoding (2-space indent, sorted keys,
// trailing newline) and the content hash used for provenance and idempotence.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/seyallius/benchbook/internal/model/canon"
)

// ---------------------------------------- Constructor(s) -------------------------------------- //

// NewCurrent returns an empty current run carrying just the schema version and
// provenance. It is the starting point for `benchbook run`.
func NewCurrent(createdAt time.Time, provenance Provenance) *Run {
	return &Run{
		SchemaVersion: SchemaVersion,
		CreatedAt:     createdAt.UTC().Truncate(time.Second),
		Provenance:    provenance,
		Benchmarks:    []Benchmark{},
		Profiles:      []Profile{},
	}
}

// Normalized returns a copy of the run with every collection sorted and every nil
// slice replaced by an empty one, so encoding is byte-stable.
func (r *Run) Normalized() *Run {
	out := *r
	out.CreatedAt = out.CreatedAt.UTC().Truncate(time.Second)
	out.Benchmarks = make([]Benchmark, len(r.Benchmarks))
	copy(out.Benchmarks, r.Benchmarks)
	for i := range out.Benchmarks {
		samples := make([]Sample, len(out.Benchmarks[i].Samples))
		copy(samples, out.Benchmarks[i].Samples)
		for j := range samples {
			if samples[j].Extra == nil {
				samples[j].Extra = map[string]float64{}
			}
		}
		out.Benchmarks[i].Samples = samples
	}
	out.Profiles = make([]Profile, len(r.Profiles))
	copy(out.Profiles, r.Profiles)
	for i := range out.Profiles {
		if out.Profiles[i].Meta == nil {
			out.Profiles[i].Meta = []string{}
		}
		if out.Profiles[i].Rows == nil {
			out.Profiles[i].Rows = []ProfileRow{}
		}
	}
	if out.Benchmarks == nil {
		out.Benchmarks = []Benchmark{}
	}
	if out.Profiles == nil {
		out.Profiles = []Profile{}
	}
	SortBenchmarks(out.Benchmarks)
	SortProfiles(out.Profiles)
	return &out
}

// ----------------------------------------- Public API ----------------------------------------- //

// NewParserError builds the error every parser returns when it found nothing usable.
// Mentioning both the source and the expected shape is what turns "0 benchmarks"
// into an actionable message.
func NewParserError(source, expected string) error {
	return fmt.Errorf("no data found in %s (expected %s)", source, expected)
}

// CanonicalJSON encodes the run deterministically: map keys sorted, 2-space
// indent and a trailing newline. This is what lands on disk and therefore what
// humans diff in pull requests.
func (r *Run) CanonicalJSON() ([]byte, error) {
	normalized := r.Normalized()
	encoded, err := canon.Encode(normalized, CanonicalIndent)
	if err != nil {
		return nil, fmt.Errorf("encode run %q: %w", normalized.ID, err)
	}
	return encoded, nil
}

// CanonicalIndent is the indentation of every generated JSON document.
const CanonicalIndent = "  "

// ContentPayload is the part of a run that participates in the content hash:
// benchmarks and profiles only. Label, Note and Provenance are deliberately
// excluded, so a promotion stays idempotent and hand edits to the label or note
// never invalidate the hash.
type ContentPayload struct {
	// Benchmarks mirrors Run.Benchmarks for hashing.
	Benchmarks []Benchmark `json:"benchmarks"`
	// Profiles mirrors Run.Profiles for hashing.
	Profiles []Profile `json:"profiles"`
}

// ComputeContentHash returns the sha256 of the canonical content payload.
func (r *Run) ComputeContentHash() (string, error) {
	normalized := r.Normalized()
	payload := ContentPayload{Benchmarks: normalized.Benchmarks, Profiles: normalized.Profiles}
	encoded, err := canon.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode content payload: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// DecodeRun parses a run document and refuses anything newer than this binary
// supports, with a message that says what to do about it. Older versions are
// routed through Migrate so the seam exists before it is needed.
func DecodeRun(reader io.Reader, source string) (*Run, error) {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("parse %s as JSON: %w", source, err)
	}
	if header.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf(
			"%s uses schema_version %d but this benchbook understands up to %d; upgrade benchbook (go install %s/cmd/benchbook@latest)",
			source, header.SchemaVersion, SchemaVersion, modulePath)
	}
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	migrated, err := Migrate(&run, header.SchemaVersion, source)
	if err != nil {
		return nil, err
	}
	return migrated.Normalized(), nil
}

// Migrate upgrades a decoded run from fromVersion to SchemaVersion. There are no
// released older versions yet, so every supported input is a no-op; the function
// exists so a future bump is a localized change instead of a refactor.
func Migrate(run *Run, fromVersion int, source string) (*Run, error) {
	switch fromVersion {
	case SchemaVersion:
		return run, nil
	default:
		if fromVersion < 0 || fromVersion > SchemaVersion {
			return nil, fmt.Errorf("%s declares an invalid schema_version %d", source, fromVersion)
		}
		return nil, fmt.Errorf(
			"%s uses schema_version %d, which predates this build (%d) and has no migration; re-record the run",
			source, fromVersion, SchemaVersion)
	}
}

// modulePath is the import path of the tool, used in upgrade hints.
const modulePath = "github.com/seyallius/benchbook"

// Slug turns an arbitrary label into a lowercase, dash-separated file-safe slug.
func Slug(label string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
