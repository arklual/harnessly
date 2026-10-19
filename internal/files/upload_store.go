package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

// Attachments people upload into a conversation: written under the uploads folder, <workspace>/<tab>/<name>, and
// removed again a day later (or at once, on request).

const (
	// ImageMaxBytes and FileMaxBytes are the largest image and other file an upload takes.
	ImageMaxBytes = 10 * 1024 * 1024
	FileMaxBytes  = 50 * 1024 * 1024
	uploadTTL     = 24 * time.Hour
)

// imageExtensions are the image types an image upload takes, by their exact content type.
var imageExtensions = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}

// ImageType says whether an image upload takes this content type. Unlike the previous runtime, a name its lookup found
// on every object (constructor, toString…) is no type.
func ImageType(mime string) bool { _, ok := imageExtensions[mime]; return ok }

// Uploads is the uploads folder.
type Uploads struct {
	Dir string
	// Now and Random make names reproducible in tests.
	Now    func() time.Time
	Random func() string
}

func (u *Uploads) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *Uploads) random() string {
	if u.Random != nil {
		return u.Random()
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// uploadID is a workspace or tab id as a folder name: its letters, digits, "_" and "-", at most 64; "unknown" for
// none.
func uploadID(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < 0x80 && isNameChar(rune(c)) {
			b.WriteByte(c)
		}
	}
	s := b.String()
	if s == "" {
		return "unknown"
	}
	return s[:min(64, len(s))]
}

func isNameChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
}

// underscored is a name with every character but letters, digits, "_" and "-" made "_" (one per UTF-16 unit, as the
// previous runtime replaced them), at most 40.
func underscored(name []rune) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x80 && isNameChar(r):
			b.WriteRune(r)
		case utf16.RuneLen(r) == 2:
			b.WriteString("__")
		default:
			b.WriteByte('_')
		}
	}
	s := b.String()
	return s[:min(40, len(s))]
}

// parsedName is a file name cut as path.parse cut it: the name and the extension of its last segment.
func parsedName(p []rune) (name, ext []rune) {
	startDot, startPart, end := -1, 0, -1
	matchedSlash, preDotState := true, 0
	start := 0
	absolute := len(p) > 0 && p[0] == '/'
	if absolute {
		start = 1
	}
	for i := len(p) - 1; i >= start; i-- {
		c := p[i]
		if c == '/' {
			if !matchedSlash {
				startPart = i + 1
				break
			}
			continue
		}
		if end == -1 {
			matchedSlash = false
			end = i + 1
		}
		if c == '.' {
			if startDot == -1 {
				startDot = i
			} else if preDotState != 1 {
				preDotState = 1
			}
		} else if startDot != -1 {
			preDotState = -1
		}
	}
	if end == -1 {
		return nil, nil
	}
	from := startPart
	if startPart == 0 && absolute {
		from = 1
	}
	if startDot == -1 || preDotState == 0 || (preDotState == 1 && startDot == end-1 && startDot == startPart+1) {
		return p[from:end], nil
	}
	return p[from:startDot], p[startDot:end]
}

// DecodeName is a file name header as the previous runtime read it: the header's bytes as Latin-1 characters, then
// decodeURIComponent; false where that threw.
func DecodeName(header string) ([]rune, bool) {
	out := make([]rune, 0, len(header))
	for i := 0; i < len(header); i++ {
		c := header[i]
		if c != '%' {
			out = append(out, rune(c))
			continue
		}
		b, ok := hexByte(header, i)
		if !ok {
			return nil, false
		}
		i += 2
		if b < 0x80 {
			out = append(out, rune(b))
			continue
		}
		n := 0
		switch {
		case b&0xE0 == 0xC0:
			n = 1
		case b&0xF0 == 0xE0:
			n = 2
		case b&0xF8 == 0xF0:
			n = 3
		default:
			return nil, false
		}
		r := rune(b) & (0x3F >> n)
		for k := 0; k < n; k++ {
			if i+1 >= len(header) || header[i+1] != '%' {
				return nil, false
			}
			c, ok := hexByte(header, i+1)
			if !ok || c&0xC0 != 0x80 {
				return nil, false
			}
			r = r<<6 | rune(c&0x3F)
			i += 3
		}
		minimum := [...]rune{0, 0x80, 0x800, 0x10000}[n]
		if r < minimum || r > 0x10FFFF || r >= 0xD800 && r <= 0xDFFF {
			return nil, false
		}
		out = append(out, r)
	}
	return out, true
}

func hexByte(s string, i int) (byte, bool) {
	if i+2 >= len(s) {
		return 0, false
	}
	h, l := unhex(s[i+1]), unhex(s[i+2])
	if h < 0 || l < 0 {
		return 0, false
	}
	return byte(h<<4 | l), true
}

// ImageName is an image's stored name: <ms>-<8 hex>-<name>.<type>.
func (u *Uploads) ImageName(original []rune, mime string) string {
	base := "image"
	if original != nil {
		name, _ := parsedName(original)
		if s := underscored(name); s != "" {
			base = s
		}
	}
	return strconv.FormatInt(u.now().UnixMilli(), 10) + "-" + u.random() + "-" + base + "." + imageExtensions[mime]
}

// FileName is a file's stored name: <ms>-<8 hex>-<name><extension, its letters, digits and dots, at most 16>.
func (u *Uploads) FileName(original []rune) string {
	var name, ext []rune
	if original != nil {
		name, ext = parsedName(original)
	}
	base := underscored(name)
	if base == "" {
		base = "file"
	}
	var e strings.Builder
	for _, r := range ext {
		if r < 0x80 && (isNameChar(r) && r != '_' && r != '-' || r == '.') {
			e.WriteRune(r)
		}
	}
	extension := e.String()
	if strings.HasPrefix(extension, ".") {
		extension = extension[:min(16, len(extension))]
	} else {
		extension = ""
	}
	return strconv.FormatInt(u.now().UnixMilli(), 10) + "-" + u.random() + "-" + base + extension
}

// errOutsideUploads: a workspace or tab folder that is a link; nothing is written through it.
var errOutsideUploads = errors.New("upload folder leads out of the uploads folder")

// Save writes an upload of a tab and gives its path. The tab's folder must be inside the uploads folder once its links
// are resolved, and the file is created new, never through a link.
func (u *Uploads) Save(data []byte, wsID, tabID, name string) (string, error) {
	ws := filepath.Join(u.Dir, uploadID(wsID))
	dir := filepath.Join(ws, uploadID(tabID))
	for _, p := range []string{ws, dir} {
		if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", errOutsideUploads
		}
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(u.Dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", errOutsideUploads
	}
	full := filepath.Join(dir, name)
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	return full, f.Close()
}

// Cleanup is what a clean-up removed: files and their bytes.
type Cleanup struct {
	Deleted    int
	FreedBytes int64
}

// Clean removes the uploads older than a day, or all of them, then the folders it emptied. Only files at
// <workspace>/<tab>/<file> are looked at, as before; a file of no bytes is removed but not counted, as before. Unlike
// before, links are never followed: a linked folder is not entered and a linked file not removed.
func (u *Uploads) Clean(all bool) Cleanup {
	var out Cleanup
	cutoff := float64(u.now().UnixMilli()) - float64(uploadTTL.Milliseconds())
	for _, ws := range realDirs(u.Dir) {
		wsPath := filepath.Join(u.Dir, ws)
		for _, tab := range realDirs(wsPath) {
			tabPath := filepath.Join(wsPath, tab)
			entries, _ := os.ReadDir(tabPath)
			for _, e := range entries {
				p := filepath.Join(tabPath, e.Name())
				st, err := os.Lstat(p)
				if err != nil || !st.Mode().IsRegular() {
					continue
				}
				if !all && float64(st.ModTime().UnixNano())/1e6 >= cutoff {
					continue
				}
				if os.Remove(p) == nil && st.Size() > 0 {
					out.Deleted++
					out.FreedBytes += st.Size()
				}
			}
			removeIfEmpty(tabPath)
		}
		removeIfEmpty(wsPath)
	}
	return out
}

// realDirs are the folders in dir that are folders themselves, not links to one.
func realDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 && e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

func removeIfEmpty(dir string) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = syscall.Rmdir(dir)
	}
}
