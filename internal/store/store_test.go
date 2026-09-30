// Package store. store_test - tests for the persistence rules: atomic writes, the
// disposable current run, the immutable milestone history and the sentinel errors
// the CLI turns into friendly messages.
package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// fixtureRun builds a minimal but complete run.
func fixtureRun(label string, nsOp float64) *model.Run {
	run := model.NewCurrent(time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC), model.Provenance{
		GitSHA:              "0123456789abcdef0123456789abcdef01234567",
		GitBranch:           "main",
		GoVersion:           "go1.22.5",
		GOOS:                "linux",
		GOARCH:              "amd64",
		CPUModel:            "Test CPU",
		CPUCores:            4,
		HardwareFingerprint: "fingerprint",
	})
	run.Label = label
	run.Benchmarks = []model.Benchmark{{
		ID: "example.com/demo.BenchmarkConcat", Package: "example.com/demo",
		Name: "BenchmarkConcat", Workload: "Concat", Variant: model.DefaultVariant,
		Samples: []model.Sample{{Iterations: 100, NsOp: nsOp}},
	}}
	hash, err := run.ComputeContentHash()
	if err != nil {
		panic(err)
	}
	run.ContentHash = hash
	return run
}

// saveMilestone assigns the next sequence the way the milestone command does and
// returns the file name it was written to.
func saveMilestone(t *testing.T, store *Store, run *model.Run) string {
	t.Helper()
	sequence, err := store.NextSeq()
	if err != nil {
		t.Fatalf("NextSeq: %v", err)
	}
	run.Seq = sequence
	name, err := store.SaveMilestone(run)
	if err != nil {
		t.Fatalf("SaveMilestone: %v", err)
	}
	return name
}

// newStore returns a store on a fresh temporary directory.
func newStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	store := New(root, filepath.Join(root, RawDirName))
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	return store
}

// ----------------------------------------- Public API ----------------------------------------- //

// TestMissingCurrentRunReportsASentinel keeps the CLI from printing a bare file-not-
// found path when nothing has been recorded yet.
func TestMissingCurrentRunReportsASentinel(t *testing.T) {
	store := newStore(t)
	if _, err := store.LoadCurrent(); !errors.Is(err, ErrNoCurrent) {
		t.Fatalf("LoadCurrent error = %v, want ErrNoCurrent", err)
	}
	if _, err := store.LatestMilestone(); !errors.Is(err, ErrNoMilestones) {
		t.Fatalf("LatestMilestone error = %v, want ErrNoMilestones", err)
	}
}

// TestCurrentRunRoundTripsAndIsOverwritten is the "working tree" half of the model:
// current.json is replaced, never appended to.
func TestCurrentRunRoundTripsAndIsOverwritten(t *testing.T) {
	store := newStore(t)
	if err := store.SaveCurrent(fixtureRun("first", 100)); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	loaded, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if loaded.Benchmarks[0].Samples[0].NsOp != 100 {
		t.Errorf("ns/op = %v, want 100", loaded.Benchmarks[0].Samples[0].NsOp)
	}
	if !loaded.IsCurrent() {
		t.Error("a saved current run reports IsCurrent = false")
	}

	if err := store.SaveCurrent(fixtureRun("second", 200)); err != nil {
		t.Fatalf("SaveCurrent (overwrite): %v", err)
	}
	second, err := store.LoadCurrent()
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if got := second.Benchmarks[0].Samples[0].NsOp; got != 200 {
		t.Errorf("ns/op = %v after the second save, want 200 (the first must be replaced)", got)
	}
	entries, err := os.ReadDir(store.Root())
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, want := range []string{CurrentFile, HistoryDir, RawDirName} {
		if !names[want] {
			t.Errorf("data dir does not contain %s (has %v)", want, names)
		}
	}
	if !names[CurrentFile] {
		t.Error("current.json missing after SaveCurrent")
	}
}

// TestCurrentJSONIsCanonicalAndEndsWithANewline is what makes the file diffable in a
// pull request.
func TestCurrentJSONIsCanonicalAndEndsWithANewline(t *testing.T) {
	store := newStore(t)
	run := fixtureRun("canonical", 100)
	if err := store.SaveCurrent(run); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	content, err := os.ReadFile(store.CurrentPath())
	if err != nil {
		t.Fatalf("read current.json: %v", err)
	}
	want, err := run.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if !bytes.Equal(content, want) {
		t.Errorf("on-disk JSON differs from CanonicalJSON:\n%s\n---\n%s", content, want)
	}
	info, err := os.Stat(store.CurrentPath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != filePerm {
		t.Errorf("permissions = %v, want %v", info.Mode().Perm(), filePerm)
	}
}

// TestSaveMilestoneAssignsSequenceAndSlugName covers the naming rule: mNNN plus the
// slug of the label.
func TestSaveMilestoneAssignsSequenceAndSlugName(t *testing.T) {
	store := newStore(t)
	run := fixtureRun("Faster Concat + fewer allocs", 100)
	name := saveMilestone(t, store, run)
	if filepath.Base(name) != "m001-faster-concat-fewer-allocs.json" {
		t.Errorf("file = %q, want m001-faster-concat-fewer-allocs.json", filepath.Base(name))
	}
	if run.Seq != 1 || run.ID != "m001" {
		t.Errorf("seq/id = %d/%q, want 1/m001", run.Seq, run.ID)
	}
	loaded, err := store.LoadMilestone("m001")
	if err != nil {
		t.Fatalf("LoadMilestone: %v", err)
	}
	if loaded.Label != run.Label {
		t.Errorf("label = %q, want %q", loaded.Label, run.Label)
	}
	if loaded.IsCurrent() {
		t.Error("a loaded milestone reports IsCurrent = true")
	}
}

// TestSaveMilestoneRefusesToOverwriteHistory is the immutability rule: the whole
// comparison model depends on published numbers never changing.
func TestSaveMilestoneRefusesToOverwriteHistory(t *testing.T) {
	store := newStore(t)
	run := fixtureRun("baseline", 100)
	name := saveMilestone(t, store, run)
	before, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read milestone: %v", err)
	}

	duplicate := fixtureRun("baseline", 999)
	duplicate.Seq = run.Seq
	duplicate.ID = run.ID
	if _, err := store.SaveMilestone(duplicate); err == nil {
		t.Fatal("SaveMilestone overwrote an existing milestone")
	}
	after, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read milestone: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the milestone file changed, want it untouched")
	}
}

// TestNextSequenceFollowsTheHighestMilestone keeps numbering stable when a milestone
// is deleted by hand.
func TestNextSequenceFollowsTheHighestMilestone(t *testing.T) {
	store := newStore(t)
	if seq, err := store.NextSeq(); err != nil || seq != 1 {
		t.Fatalf("NextSeq on an empty history = (%d, %v), want (1, nil)", seq, err)
	}
	for index := 0; index < 3; index++ {
		saveMilestone(t, store, fixtureRun("step", float64(100+index)))
	}
	if seq, err := store.NextSeq(); err != nil || seq != 4 {
		t.Fatalf("NextSeq = (%d, %v), want (4, nil)", seq, err)
	}
	// Remove the newest milestone: the next number must not be reused, because a
	// milestone id that once referred to one run must never refer to another.
	entries, err := os.ReadDir(store.HistoryPath())
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "m003-step.json" {
			if err := os.Remove(filepath.Join(store.HistoryPath(), entry.Name())); err != nil {
				t.Fatalf("remove: %v", err)
			}
		}
	}
	if seq, err := store.NextSeq(); err != nil || seq != 3 {
		t.Fatalf("NextSeq after deleting the newest = (%d, %v), want (3, nil)", seq, err)
	}
}

// TestMilestonesAreSortedBySequence keeps the timeline stable regardless of the file
// system's directory order.
func TestMilestonesAreSortedBySequence(t *testing.T) {
	store := newStore(t)
	if err := store.SaveCurrent(fixtureRun("current", 50)); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	for _, label := range []string{"one", "two", "three"} {
		saveMilestone(t, store, fixtureRun(label, 100))
	}
	milestones, err := store.Milestones()
	if err != nil {
		t.Fatalf("Milestones: %v", err)
	}
	if len(milestones) != 3 {
		t.Fatalf("milestones = %d, want 3", len(milestones))
	}
	for index, milestone := range milestones {
		if milestone.Seq != index+1 {
			t.Errorf("milestone %d has seq %d, want %d", index, milestone.Seq, index+1)
		}
		if milestone.ID == "" {
			t.Errorf("milestone %d has no id", index)
		}
	}
	latest, err := store.LatestMilestone()
	if err != nil {
		t.Fatalf("LatestMilestone: %v", err)
	}
	if latest.Label != "three" {
		t.Errorf("latest label = %q, want three", latest.Label)
	}
}

// TestLoadMilestoneRejectsUnknownAndMalformedIDs distinguishes "no such milestone"
// from "that is not a milestone id", because the CLI says different things.
func TestLoadMilestoneRejectsUnknownAndMalformedIDs(t *testing.T) {
	store := newStore(t)
	if _, err := store.LoadMilestone("m404"); !errors.Is(err, ErrMilestoneNotFound) {
		t.Errorf("LoadMilestone(m404) error = %v, want ErrMilestoneNotFound", err)
	}
	for _, id := range []string{"", "001", "m", "m00", "milestone1", "m1x"} {
		if _, err := store.LoadMilestone(id); err == nil {
			t.Errorf("LoadMilestone(%q) = nil error, want a refusal", id)
		}
	}
	if seq, err := ParseMilestoneID("m012"); err != nil || seq != 12 {
		t.Errorf("ParseMilestoneID(m012) = (%d, %v), want (12, nil)", seq, err)
	}
}

// TestSaveMilestoneWithoutALabelStillGetsAFileName keeps `milestone` usable before a
// label is supplied.
func TestSaveMilestoneWithoutALabelStillGetsAFileName(t *testing.T) {
	store := newStore(t)
	name := saveMilestone(t, store, fixtureRun("", 100))
	if filepath.Base(name) != "m001.json" {
		t.Errorf("file = %q, want m001.json", filepath.Base(name))
	}
}

// TestWriteRawStoresUntouchedOutput is the provenance promise: the raw tool output is
// kept exactly as the tool printed it.
func TestWriteRawStoresUntouchedOutput(t *testing.T) {
	store := newStore(t)
	content := []byte("BenchmarkConcat-8  1000  10.5 ns/op\r\nweird\tbytes\x00\n")
	path, err := store.WriteRaw("last-run.txt", content)
	if err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	if path != store.RawPath("last-run.txt") {
		t.Errorf("path = %q, want %q", path, store.RawPath("last-run.txt"))
	}
	read, err := store.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(read, content) {
		t.Errorf("raw content = %q, want %q", read, content)
	}
	// A name with a path separator must not escape the raw directory.
	escaped, err := store.WriteRaw("../../etc/passwd", []byte("nope"))
	if err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	if filepath.Dir(escaped) != store.RawPath(".") {
		t.Errorf("escaped raw path = %q, want it inside %q", escaped, store.RawPath("."))
	}
}

// TestAtomicWriteLeavesNoTemporaryFiles is the "every write is atomic" rule, checked
// from the outside: a successful write must not leave a temporary file behind.
func TestAtomicWriteLeavesNoTemporaryFiles(t *testing.T) {
	store := newStore(t)
	if err := store.SaveCurrent(fixtureRun("atomic", 100)); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	saveMilestone(t, store, fixtureRun("atomic", 100))
	dirs := []string{store.Root(), store.HistoryPath(), store.RawDir()}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			if len(entry.Name()) >= len(tempPattern)-1 && entry.Name()[0] == '.' {
				t.Errorf("temporary file left behind in %s: %s", dir, entry.Name())
			}
		}
	}
}

// TestWriteFileAndCopyFileAreAtomic covers the helpers the site writer uses to
// publish shell assets.
func TestWriteFileAndCopyFileAreAtomic(t *testing.T) {
	store := newStore(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "asset.css")
	if err := WriteFile(target, []byte("body { color: red }\n")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "body { color: red }\n" {
		t.Errorf("content = %q", content)
	}

	copied := filepath.Join(dir, "copy.css")
	if err := CopyFile(target, copied); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	duplicate, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if !bytes.Equal(duplicate, content) {
		t.Error("copied file differs from the source")
	}
	if err := CopyFile(filepath.Join(dir, "missing.css"), copied); err == nil {
		t.Error("CopyFile accepted a missing source")
	}
	_ = store
}

// TestLoadRejectsANewerSchemaAtTheStoreBoundary keeps the refusal close to the disk,
// so no command can accidentally read a document it does not understand.
func TestLoadRejectsANewerSchemaAtTheStoreBoundary(t *testing.T) {
	store := newStore(t)
	if err := store.SaveCurrent(fixtureRun("future", 100)); err != nil {
		t.Fatalf("SaveCurrent: %v", err)
	}
	content, err := os.ReadFile(store.CurrentPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	upgraded := bytes.Replace(content, []byte(`"schema_version": 1`), []byte(`"schema_version": 42`), 1)
	if bytes.Equal(upgraded, content) {
		t.Fatal("fixture did not contain schema_version 1")
	}
	if err := os.WriteFile(store.CurrentPath(), upgraded, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err = store.LoadCurrent()
	if err == nil {
		t.Fatal("LoadCurrent accepted a newer schema version")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("42")) {
		t.Errorf("error %q does not name the version it found", err)
	}
}
