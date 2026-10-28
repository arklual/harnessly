package handoff

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	artifactDraftTTL   = 24 * time.Hour
	artifactClaimedTTL = 24 * time.Hour
	artifactBuffer     = 64 * 1024
)

var (
	ErrNoCompleteHistory = errors.New("handoff: transcript has no complete record")
	ErrArtifactNotFound  = errors.New("handoff: artifact not found")
	artifactIDPattern    = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// ArtifactStore keeps handoff material in private server-owned paths. Draft history is bound to both conversations;
// claimed material is bound to the conversation whose CLI may read it.
type ArtifactStore struct {
	root string
	now  func() time.Time
}

// NewArtifactStore opens the private handoff store below dataDir and removes expired material left by a previous
// runtime. Claimed material is normally released when its first turn ends; its TTL bounds crash leftovers.
func NewArtifactStore(dataDir string) (*ArtifactStore, error) {
	s := &ArtifactStore{root: filepath.Join(dataDir, "v2", "handoffs"), now: time.Now}
	for _, dir := range []string{s.root, s.draftsDir(), s.claimedDir()} {
		if err := privateDir(dir); err != nil {
			return nil, err
		}
	}
	if err := s.cleanupStale(); err != nil {
		return nil, err
	}
	return s, nil
}

func privateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func (s *ArtifactStore) draftsDir() string  { return filepath.Join(s.root, "drafts") }
func (s *ArtifactStore) claimedDir() string { return filepath.Join(s.root, "claimed") }

func artifactOwner(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:16])
}

func (s *ArtifactStore) draftDir(sourceID, targetID string) string {
	return filepath.Join(s.draftsDir(), artifactOwner(sourceID), targetID)
}

// Target returns the server-reserved destination of a draft without exposing a path to the page.
func (s *ArtifactStore) Target(sourceID, artifactID string) (string, error) {
	if !wire.ValidConvID(sourceID) || !validArtifactPart(artifactID) {
		return "", ErrArtifactNotFound
	}
	matches, err := filepath.Glob(filepath.Join(s.draftsDir(), artifactOwner(sourceID), "tab-*", artifactID+".jsonl"))
	if err != nil || len(matches) != 1 {
		return "", ErrArtifactNotFound
	}
	targetID := filepath.Base(filepath.Dir(matches[0]))
	if !wire.ValidConvID(targetID) {
		return "", ErrArtifactNotFound
	}
	return targetID, nil
}

func (s *ArtifactStore) draftPath(sourceID, artifactID, targetID string) string {
	return filepath.Join(s.draftDir(sourceID, targetID), artifactID+".jsonl")
}

func (s *ArtifactStore) claimedPath(targetID, artifactID string) string {
	return filepath.Join(s.claimedDir(), artifactOwner(targetID), artifactID+".jsonl")
}

func validArtifactPart(id string) bool { return artifactIDPattern.MatchString(id) }

func nextArtifactID() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// Snapshot reads the transcript size observed at open time, converts every complete record to provider-neutral
// semantic JSONL, and discards only an actively-written trailing record. It never loads the transcript as a whole.
func (s *ArtifactStore) Snapshot(sourceID, transcriptPath, targetID string) (wire.HandoffArtifact, error) {
	if !wire.ValidConvID(sourceID) || !wire.ValidConvID(targetID) {
		return wire.HandoffArtifact{}, errors.New("handoff: invalid conversation id")
	}
	if err := s.cleanupStale(); err != nil {
		return wire.HandoffArtifact{}, err
	}
	source, err := os.Open(transcriptPath)
	if err != nil {
		return wire.HandoffArtifact{}, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return wire.HandoffArtifact{}, err
	}
	id, err := nextArtifactID()
	if err != nil {
		return wire.HandoffArtifact{}, err
	}
	dir := s.draftDir(sourceID, targetID)
	if err := privateDir(dir); err != nil {
		return wire.HandoffArtifact{}, err
	}
	tmp := filepath.Join(dir, "."+id+".tmp")
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return wire.HandoffArtifact{}, err
	}
	keep, err := completeJSONLSize(source, info.Size())
	written := int64(0)
	records := 0
	if err == nil && keep == 0 {
		err = ErrNoCompleteHistory
	}
	if err == nil {
		records, written, err = canonicalHistory(file, io.LimitReader(source, keep))
		if err == nil && records == 0 {
			err = ErrNoCompleteHistory
		}
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return wire.HandoffArtifact{}, err
	}
	target := s.draftPath(sourceID, id, targetID)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return wire.HandoffArtifact{}, err
	}
	return wire.HandoffArtifact{ID: id, Bytes: written}, nil
}

func completeJSONLSize(file *os.File, size int64) (int64, error) {
	buf := make([]byte, artifactBuffer)
	for end := size; end > 0; {
		start := max(int64(0), end-int64(len(buf)))
		chunk := buf[:end-start]
		n, err := file.ReadAt(chunk, start)
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if n != len(chunk) {
			return 0, io.ErrUnexpectedEOF
		}
		if at := bytes.LastIndexByte(chunk, '\n'); at >= 0 {
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return 0, err
			}
			return start + int64(at) + 1, nil
		}
		end = start
	}
	return 0, nil
}

// Claim atomically moves a draft to the target conversation. A draft can be claimed only once and only by the source
// and target pair it was created for.
func (s *ArtifactStore) Claim(sourceID, artifactID, targetID string) (string, int64, error) {
	if !wire.ValidConvID(sourceID) || !wire.ValidConvID(targetID) || !validArtifactPart(artifactID) {
		return "", 0, ErrArtifactNotFound
	}
	from := s.draftPath(sourceID, artifactID, targetID)
	info, err := os.Stat(from)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, ErrArtifactNotFound
	}
	if err != nil {
		return "", 0, err
	}
	dir := filepath.Join(s.claimedDir(), artifactOwner(targetID))
	if err := privateDir(dir); err != nil {
		return "", 0, err
	}
	to := s.claimedPath(targetID, artifactID)
	now := s.now()
	if err := os.Chtimes(from, now, now); err != nil {
		return "", 0, err
	}
	if err := os.Rename(from, to); errors.Is(err, os.ErrNotExist) {
		return "", 0, ErrArtifactNotFound
	} else if err != nil {
		return "", 0, err
	}
	return to, info.Size(), nil
}

// PutText stores approved editable handoff text directly for a target conversation and returns its private path.
func (s *ArtifactStore) PutText(targetID, text string) (string, int64, error) {
	if !wire.ValidConvID(targetID) || text == "" {
		return "", 0, errors.New("handoff: invalid artifact text")
	}
	id, err := nextArtifactID()
	if err != nil {
		return "", 0, err
	}
	dir := filepath.Join(s.claimedDir(), artifactOwner(targetID))
	if err := privateDir(dir); err != nil {
		return "", 0, err
	}
	tmp := filepath.Join(dir, "."+id+".tmp")
	if err := os.WriteFile(tmp, []byte(text), 0o600); err != nil {
		return "", 0, err
	}
	target := s.claimedPath(targetID, id)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}
	return target, int64(len(text)), nil
}

// Release removes claimed material after its target conversation no longer needs it. Both the target id and the exact
// server-owned path must match, so persisted state cannot make the runtime remove an arbitrary file. It is idempotent.
func (s *ArtifactStore) Release(targetID, path string) error {
	if !wire.ValidConvID(targetID) {
		return ErrArtifactNotFound
	}
	clean := filepath.Clean(path)
	ownerDir := filepath.Join(s.claimedDir(), artifactOwner(targetID))
	base := filepath.Base(clean)
	id := strings.TrimSuffix(base, ".jsonl")
	if filepath.Dir(clean) != ownerDir || !strings.HasSuffix(base, ".jsonl") || !validArtifactPart(id) {
		return ErrArtifactNotFound
	}
	if err := os.Remove(clean); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove claimed handoff artifact: %w", err)
	}
	_ = os.Remove(ownerDir) // empty owner directories carry no state; a non-empty one is intentionally kept.
	return nil
}

func (s *ArtifactStore) cleanupStale() error {
	if err := cleanupBefore(s.draftsDir(), s.now().Add(-artifactDraftTTL), "draft"); err != nil {
		return err
	}
	return cleanupBefore(s.claimedDir(), s.now().Add(-artifactClaimedTTL), "claimed artifact")
}

func cleanupBefore(root string, cutoff time.Time, kind string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale handoff %s: %w", kind, err)
		}
		return nil
	})
}

// ErrArtifactTooBig: the material is larger than the caller allows.
var ErrArtifactTooBig = errors.New("handoff: artifact is too big")

// Take reads a draft in full and removes it, for material that leaves the machine (a move to another one): it exists on
// disk for as long as it takes to read it. A draft larger than max bytes is removed unread.
func (s *ArtifactStore) Take(sourceID, artifactID, targetID string, max int64) ([]byte, error) {
	if !wire.ValidConvID(sourceID) || !wire.ValidConvID(targetID) || !validArtifactPart(artifactID) {
		return nil, ErrArtifactNotFound
	}
	path := s.draftPath(sourceID, artifactID, targetID)
	defer func() {
		_ = os.Remove(path)
		_ = os.Remove(s.draftDir(sourceID, targetID))
	}()
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrArtifactNotFound
	}
	if err != nil {
		return nil, err
	}
	if info.Size() > max {
		return nil, ErrArtifactTooBig
	}
	return os.ReadFile(path)
}
