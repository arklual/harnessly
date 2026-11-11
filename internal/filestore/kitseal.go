package filestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// KitSealFile is the seal of a published kit version: `<id>/<version>/kit.sealed.json`, the list of the
// version's files with their sizes and sha256 and one hash over the list. It is written last. Until it exists a version
// is still being laid out (files may be added, none changed); once it exists the folder takes nothing that it does not
// list, and a machine counts the version as installed only when every listed file is there and matches.
const KitSealFile = "kit.sealed.json"

const (
	kitSealFormat = "kit-sealed"
	kitSealSchema = 1
	// MaxKitSealFiles and MaxKitSealBytes cap what a seal may list and how big its file may be.
	MaxKitSealFiles = 20000
	MaxKitSealBytes = 8 << 20
)

// ErrSealed: the write is not taken because the version it is in is sealed (a file the seal does not list) or the seal
// cannot be taken yet (it lists a file the room does not have).
var ErrSealed = errors.New("the kit version is sealed or the seal is not complete")

// KitSealedFile is one file a seal lists.
type KitSealedFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// KitSeal is the content of KitSealFile.
type KitSeal struct {
	Format string          `json:"format"`
	Schema int             `json:"schema"`
	Files  []KitSealedFile `json:"files"`
	SHA256 string          `json:"sha256"`
	// Legacy: the version was on the machine before seals existed (a kit that was there before kits were shared
	// opens to the team for viewing, once, when its owner's machine puts it in the room).
	Legacy bool `json:"legacy,omitempty"`
}

// NewKitSeal builds a seal over files (sorted by path here).
func NewKitSeal(files []KitSealedFile) KitSeal {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	s := KitSeal{Format: kitSealFormat, Schema: kitSealSchema, Files: files}
	s.SHA256 = s.Sum()
	return s
}

// Sum is the hash of the sorted "path size sha256" lines.
func (s KitSeal) Sum() string {
	files := append([]KitSealedFile(nil), s.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\t%d\t%s\n", f.Path, f.Size, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ParseKitSeal reads and checks a seal: its format, its hash, clean unique paths that are files of a kit version.
func ParseKitSeal(raw []byte) (KitSeal, error) {
	var s KitSeal
	if len(raw) > MaxKitSealBytes {
		return s, errors.New("the seal is too big")
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	if s.Format != kitSealFormat || s.Schema != kitSealSchema {
		return s, errors.New("not a kit seal of this schema")
	}
	if len(s.Files) > MaxKitSealFiles {
		return s, errors.New("the seal lists too many files")
	}
	seen := map[string]bool{}
	for _, f := range s.Files {
		if !SafeRel(f.Path) || f.Path == KitSealFile || seen[f.Path] || f.Size < 0 || !ValidSha(f.SHA256) {
			return s, fmt.Errorf("bad file in the seal: %q", f.Path)
		}
		seen[f.Path] = true
	}
	if s.SHA256 != s.Sum() {
		return s, errors.New("the seal's hash does not match its list")
	}
	return s, nil
}

// KitSealPrefix is "<id>/<version>/" of a path in a kits room, "" when the path is not a file of a version.
func KitSealPrefix(rel string) string {
	parts := strings.SplitN(rel, "/", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[0] + "/" + parts[1] + "/"
}
