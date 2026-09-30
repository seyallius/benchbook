// Package store. store - filesystem persistence for the current run, the immutable
// milestone history and the raw input files. Every write goes through an atomic
// temp-file-then-rename helper, so an interrupted build can never leave a
// half-written JSON document behind.
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Layout names inside the data directory.
const (
	// CurrentFile is the file holding the disposable working run.
	CurrentFile = "current.json"
	// HistoryDir holds the immutable milestone documents.
	HistoryDir = "history"
	// RawDirName holds untouched tool output (go test text, pprof text).
	RawDirName = "raw"
)

// File permissions. JSON documents are world-readable because generated docs are
// published; directories keep the same contract.
const (
	// filePerm is the mode for written documents.
	filePerm fs.FileMode = 0o644
	// dirPerm is the mode for created directories.
	dirPerm fs.FileMode = 0o755
	// tempPattern is the prefix of the temporary files used for atomic writes.
	tempPattern = ".benchbook-tmp-*"
)

// Sentinel errors. They exist so the CLI can turn a missing artifact into a
// friendly, actionable message instead of a stack of wrapped paths.
var (
	// ErrNoCurrent means no run has been recorded yet.
	ErrNoCurrent = errors.New("no current run recorded")
	// ErrNoMilestones means the history is empty.
	ErrNoMilestones = errors.New("no milestones recorded")
	// ErrMilestoneNotFound means the requested milestone id does not exist.
	ErrMilestoneNotFound = errors.New("milestone not found")
	// ErrInvalidMilestone means the id is syntactically not a milestone id.
	ErrInvalidMilestone = errors.New("invalid milestone id")
)

// milestoneName matches the generated file name "m012-some-label.json".
var milestoneName = regexp.MustCompile(`^m(\d+)(?:-(.*))?\.json$`)

// milestoneDigits is the zero padding of the sequence number in file names.
const milestoneDigits = 3

// Store is a handle on one data directory. It is safe for sequential use; the CLI
// is single-threaded by design.
type Store struct {
	// root is the absolute data directory.
	root string
	// rawDir is the absolute raw-output directory, which may live outside root.
	rawDir string
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// New returns a Store rooted at the given data directory. The raw-output directory
// may differ from the data directory, because raw text is usually gitignored while
// the JSON history is committed.
func New(root, rawDir string) *Store {
	if strings.TrimSpace(rawDir) == "" {
		rawDir = filepath.Join(root, RawDirName)
	}
	return &Store{root: root, rawDir: rawDir}
}

// ----------------------------------------- Public API ----------------------------------------- //

// Root returns the data directory.
func (s *Store) Root() string { return s.root }

// CurrentPath returns the absolute path of current.json.
func (s *Store) CurrentPath() string { return filepath.Join(s.root, CurrentFile) }

// HistoryPath returns the absolute path of the milestone directory.
func (s *Store) HistoryPath() string { return filepath.Join(s.root, HistoryDir) }

// RawPath returns the absolute path of a raw file.
func (s *Store) RawPath(name string) string { return filepath.Join(s.rawDir, filepath.Base(name)) }

// RawDir returns the absolute raw-output directory.
func (s *Store) RawDir() string { return s.rawDir }

// EnsureDirs creates the data, history and raw directories.
func (s *Store) EnsureDirs() error {
	for _, dir := range []string{s.root, s.HistoryPath(), s.rawDir} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}

// LoadCurrent reads current.json. The returned error wraps ErrNoCurrent when the
// file simply does not exist, which the CLI reports as "run `benchbook run` first".
func (s *Store) LoadCurrent() (*model.Run, error) {
	return s.loadFile(s.CurrentPath())
}

// SaveCurrent writes current.json, replacing whatever was there. This is the only
// operation that overwrites a run: the working tree is disposable by design.
func (s *Store) SaveCurrent(run *model.Run) error {
	if run == nil {
		return fmt.Errorf("save current run: run is nil")
	}
	if err := s.writeJSON(s.CurrentPath(), run); err != nil {
		return err
	}
	return nil
}

// Milestones returns every milestone in ascending sequence order. Files that do
// not match the naming convention are ignored, so a stray note in history/ cannot
// break a build.
func (s *Store) Milestones() ([]*model.Run, error) {
	entries, err := os.ReadDir(s.HistoryPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []*model.Run{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.HistoryPath(), err)
	}
	milestones := make([]*model.Run, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !milestoneName.MatchString(entry.Name()) {
			continue
		}
		run, err := s.loadFile(filepath.Join(s.HistoryPath(), entry.Name()))
		if err != nil {
			return nil, err
		}
		run.ID = milestoneID(seqOf(entry.Name()))
		milestones = append(milestones, run)
	}
	sort.SliceStable(milestones, func(i, j int) bool { return milestones[i].Seq < milestones[j].Seq })
	return milestones, nil
}

// LatestMilestone returns the highest-sequence milestone, or an error wrapping
// ErrNoMilestones when history is empty.
func (s *Store) LatestMilestone() (*model.Run, error) {
	milestones, err := s.Milestones()
	if err != nil {
		return nil, err
	}
	if len(milestones) == 0 {
		return nil, fmt.Errorf("%w in %s (promote a run with `benchbook milestone -m \"<label>\"`)", ErrNoMilestones, s.HistoryPath())
	}
	return milestones[len(milestones)-1], nil
}

// LoadMilestone reads one milestone by id ("m002") or by file name
// ("m002-sync-pool.json").
func (s *Store) LoadMilestone(id string) (*model.Run, error) {
	sequence, err := ParseMilestoneID(id)
	if err != nil {
		return nil, err
	}
	milestones, err := s.Milestones()
	if err != nil {
		return nil, err
	}
	for _, milestone := range milestones {
		if milestone.Seq == sequence {
			return milestone, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrMilestoneNotFound, id)
}

// NextSeq returns the sequence number for the next milestone. Gaps are fine: the
// maximum is used, never the count, so deleting a milestone can never cause a
// number to be reused for different content.
func (s *Store) NextSeq() (int, error) {
	milestones, err := s.Milestones()
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, milestone := range milestones {
		if milestone.Seq > highest {
			highest = milestone.Seq
		}
	}
	return highest + 1, nil
}

// SaveMilestone writes a milestone document. The caller is responsible for having
// set Seq and Label; the file name is derived from both. Existing files are never
// overwritten: a milestone is immutable.
func (s *Store) SaveMilestone(run *model.Run) (string, error) {
	if run == nil {
		return "", fmt.Errorf("save milestone: run is nil")
	}
	if run.Seq <= 0 {
		return "", fmt.Errorf("save milestone: sequence must be positive, got %d", run.Seq)
	}
	run.ID = milestoneID(run.Seq)
	path := filepath.Join(s.HistoryPath(), milestoneFileName(run.Seq, run.Label))
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("refuse to overwrite immutable milestone %s: existing milestones are never modified", path)
	}
	if err := s.writeJSON(path, run); err != nil {
		return "", err
	}
	return path, nil
}

// WriteRaw stores the untouched output of a tool under the raw directory and
// returns the path it was written to.
func (s *Store) WriteRaw(name string, content []byte) (string, error) {
	if err := os.MkdirAll(s.rawDir, dirPerm); err != nil {
		return "", fmt.Errorf("create directory %s: %w", s.rawDir, err)
	}
	path := s.RawPath(name)
	if err := writeFileAtomic(path, content); err != nil {
		return "", err
	}
	return path, nil
}

// ReadFile reads a file from the data directory, used for raw input ingestion.
func (s *Store) ReadFile(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return content, nil
}

// ParseMilestoneID accepts "m002", "m2", "m002-sync-pool.json" and returns the
// sequence number.
func ParseMilestoneID(id string) (int, error) {
	trimmed := strings.TrimSpace(id)
	trimmed = strings.TrimSuffix(trimmed, ".json")
	if !strings.HasPrefix(trimmed, "m") {
		return 0, fmt.Errorf("%w: %q (expected something like m002)", ErrInvalidMilestone, id)
	}
	digits := trimmed[1:]
	if index := strings.IndexByte(digits, '-'); index >= 0 {
		digits = digits[:index]
	}
	sequence, err := strconv.Atoi(digits)
	if err != nil || sequence <= 0 {
		return 0, fmt.Errorf("%w: %q (expected something like m002)", ErrInvalidMilestone, id)
	}
	return sequence, nil
}

// milestoneID renders the canonical id of a milestone.
func milestoneID(sequence int) string {
	return fmt.Sprintf("m%0*d", milestoneDigits, sequence)
}

// milestoneFileName renders "m002-sync-pool.json", falling back to "m002.json"
// when the label has no usable characters.
func milestoneFileName(sequence int, label string) string {
	slug := model.Slug(label)
	if slug == "" {
		return milestoneID(sequence) + ".json"
	}
	return fmt.Sprintf("%s-%s.json", milestoneID(sequence), slug)
}

// seqOf extracts the sequence number from a milestone file name.
func seqOf(fileName string) int {
	match := milestoneName.FindStringSubmatch(fileName)
	if match == nil {
		return 0
	}
	sequence, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return sequence
}

// loadFile reads and decodes a run document, wrapping the "missing file" case in
// the sentinel that matches the file's role.
func (s *Store) loadFile(path string) (*model.Run, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if filepath.Base(path) == CurrentFile {
				return nil, fmt.Errorf("%w at %s (record one with `benchbook run`)", ErrNoCurrent, path)
			}
			return nil, fmt.Errorf("%w at %s", ErrMilestoneNotFound, path)
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	run, err := model.DecodeRun(file, path)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// writeJSON serialises a run canonically and writes it atomically.
func (s *Store) writeJSON(path string, run *model.Run) error {
	content, err := run.CanonicalJSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("create directory %s: %w", filepath.Dir(path), err)
	}
	if err := writeFileAtomic(path, content); err != nil {
		return err
	}
	return nil
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// writeFileAtomic writes content to a temporary file in the destination directory
// and renames it into place. rename(2) is atomic within a filesystem, so readers
// see either the old file or the new one, never a partial write; if anything fails
// the temporary file is removed.
func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return fmt.Errorf("create temporary file next to %s: %w", path, err)
	}
	tempName := temp.Name()
	cleanup := func() {
		temp.Close()
		os.Remove(tempName)
	}
	if _, err := temp.Write(content); err != nil {
		cleanup()
		return fmt.Errorf("write %s: %w", tempName, err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempName)
		return fmt.Errorf("close %s: %w", tempName, err)
	}
	if err := os.Chmod(tempName, filePerm); err != nil {
		os.Remove(tempName)
		return fmt.Errorf("set permissions on %s: %w", tempName, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		os.Remove(tempName)
		return fmt.Errorf("move %s into place as %s: %w", tempName, path, err)
	}
	return nil
}

// CopyFile copies src to dst atomically; used to publish milestone JSON for the
// in-browser comparer.
func CopyFile(src, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return fmt.Errorf("create directory %s: %w", filepath.Dir(dst), err)
	}
	return writeFileAtomic(dst, content)
}

// WriteFile writes content atomically, creating parent directories as needed. It
// is the single write path the site writer uses, which is what makes
// "build twice, compare bytes" a meaningful test.
func WriteFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("create directory %s: %w", filepath.Dir(path), err)
	}
	return writeFileAtomic(path, content)
}
