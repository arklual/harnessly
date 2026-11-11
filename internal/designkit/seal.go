package designkit

// The seal of a published version: <id>/<version>/kit.sealed.json lists the version's files with sizes and
// sha256 and a hash over the list. It is written last by every install, so a version that arrives file by file from
// another machine (the kits collection of the hub) counts as installed only when its seal is there and every listed
// file is in place and matches; a file the seal does not list is not part of the version (not served, not listed, not
// exported) and does not sync.
//
// Versions installed before seals existed have none. The runtime seals them in one pass at start (SealLegacy), then
// writes SealRequired into the kits folder: from then on a version without a seal is one still arriving. Without that
// mark (a kits folder no runtime has started on, a test's folder) a version without a seal is installed as it always was.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// SealFile is the seal's name inside a version; SealRequired the mark in the kits folder that versions must be sealed.
const (
	SealFile     = filestore.KitSealFile
	SealRequired = ".seal-required"
)

// sealTTL is how long a verified version is trusted before its files are looked at again.
const sealTTL = 10 * time.Second

type sealState int

const (
	sealNone sealState = iota // no seal
	sealOK                    // sealed, every file in place and matching
	sealBad                   // sealed, but a listed file is missing or differs
)

type sealInfo struct {
	state    sealState
	set      map[string]bool
	files    int
	bytes    int64
	sealStat string
	sig      string
	at       time.Time
}

var (
	sealMu    sync.Mutex
	sealCache = map[string]*sealInfo{}
)

func sealRequired(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, SealRequired))
	return err == nil
}

// sealable tells whether a file of a version is one that syncs (and so one the seal lists).
func sealable(id, version, rel string) bool {
	return filestore.Kits{}.Synced(id + "/" + version + "/" + rel)
}

// BuildSeal lists a version's folder: every file that syncs, hashed.
func BuildSeal(id, version, root string) (filestore.KitSeal, error) {
	var files []filestore.KitSealedFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == SealFile || !sealable(id, version, rel) {
			return nil
		}
		sum, size, err := sealHash(p)
		if err != nil {
			return err
		}
		files = append(files, filestore.KitSealedFile{Path: rel, Size: size, SHA256: sum})
		return nil
	})
	if err != nil {
		return filestore.KitSeal{}, err
	}
	if len(files) > filestore.MaxKitSealFiles {
		return filestore.KitSeal{}, errors.New("the kit has too many files to seal")
	}
	return filestore.NewKitSeal(files), nil
}

func sealHash(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// WriteSeal seals the version in root.
func WriteSeal(id, version, root string) error { return writeSeal(id, version, root, false) }

func writeSeal(id, version, root string, legacy bool) error {
	seal, err := BuildSeal(id, version, root)
	if err != nil {
		return err
	}
	seal.Legacy = legacy
	raw, _ := json.MarshalIndent(seal, "", " ")
	tmp := filepath.Join(root, "."+SealFile+".tmp")
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(root, SealFile))
}

// SealLegacy seals every version in dir that has no seal yet and marks the folder (SealRequired). It runs once at the
// runtime's start, before the kits are synced; running it again changes nothing. It returns how many it sealed.
//
// A folder that already has the mark is left alone without being read: a version without a seal there is one still
// arriving, never a legacy one, and listing the folder checks every sealed version by hashing its files, which held
// the start of a runtime with many kits for more than a minute before its gate listened.
func SealLegacy(dir string) (int, error) {
	if dir == "" {
		return 0, nil
	}
	if _, err := os.Stat(filepath.Join(dir, SealRequired)); err == nil {
		return 0, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	n := 0
	for _, k := range List(dir) {
		for _, v := range k.Versions {
			root := filepath.Join(dir, k.ID, v.Version)
			if _, err := os.Stat(filepath.Join(root, SealFile)); err == nil {
				continue
			}
			if err := writeSeal(k.ID, v.Version, root, true); err != nil {
				return n, err
			}
			n++
		}
	}
	if err := os.WriteFile(filepath.Join(dir, SealRequired), []byte("versions without kit.sealed.json are still arriving\n"), 0o644); err != nil {
		return n, err
	}
	return n, nil
}

// sealOf is the state of a version's seal: read and checked (cached for sealTTL and while nothing in the folder
// changes), its files looked at by size and time and, when those moved, hashed again.
func sealOf(id, version, root string) *sealInfo {
	st, err := os.Stat(filepath.Join(root, SealFile))
	if err != nil {
		return &sealInfo{state: sealNone}
	}
	stat := st.ModTime().String() + "/" + itoa(st.Size())
	sealMu.Lock()
	defer sealMu.Unlock()
	if c := sealCache[root]; c != nil && c.sealStat == stat && time.Since(c.at) < sealTTL {
		return c
	}
	raw, err := os.ReadFile(filepath.Join(root, SealFile))
	seal, perr := filestore.ParseKitSeal(raw)
	if err != nil || perr != nil {
		return &sealInfo{state: sealBad}
	}
	var sig strings.Builder
	for _, f := range seal.Files {
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != f.Size {
			return cacheSeal(root, &sealInfo{state: sealBad, sealStat: stat})
		}
		sig.WriteString(itoa(fi.ModTime().UnixNano()) + ";")
	}
	info := &sealInfo{state: sealOK, set: map[string]bool{}, files: len(seal.Files), sealStat: stat, sig: sig.String()}
	if c := sealCache[root]; c != nil && c.state == sealOK && c.sealStat == stat && c.sig == info.sig {
		info.set, info.bytes = c.set, c.bytes // nothing moved since the files were hashed
		info.at = time.Now()
		sealCache[root] = info
		return info
	}
	for _, f := range seal.Files {
		sum, _, err := sealHash(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil || sum != f.SHA256 {
			return cacheSeal(root, &sealInfo{state: sealBad, sealStat: stat})
		}
		info.set[f.Path] = true
		info.bytes += f.Size
	}
	return cacheSeal(root, info)
}

func cacheSeal(root string, info *sealInfo) *sealInfo {
	info.at = time.Now()
	sealCache[root] = info
	return info
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// installed tells whether a published version counts as installed: sealed and whole, or (without the mark) not sealed.
// seal is its state for the callers that also want the file set; nil for an unsealed version.
func installed(dir, id, version, root string) (ok bool, seal *sealInfo) {
	info := sealOf(id, version, root)
	switch info.state {
	case sealOK:
		return true, info
	case sealNone:
		return !sealRequired(dir), nil
	}
	return false, nil
}
