package apps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A package as one file: a gzip-compressed tar of the package's regular files. The team's catalog ships packages between
// machines in this form, so it is read as what it is, bytes from outside: only regular files and folders, names that
// stay inside the folder, a bounded number and size, and the result checked as any package before it is used.

// MaxArchiveBytes is the most a packed package may weigh, the bytes that travel.
const MaxArchiveBytes = 16 << 20

// ErrArchive: the bytes are not a package archive this host unpacks.
var ErrArchive = errors.New("apps: not a usable package archive")

// maxArchiveName is the longest file name a plain tar header holds. A longer one (or a name with a character beyond
// ASCII) makes archive/tar write a PAX or GNU header, which the platform's hub and Unpack refuse.
const maxArchiveName = 100

// ArchiveNameProblem says why a file's name (slash-separated, relative to the package) cannot go in an archive; "" when
// it can: the name must be ASCII and at most 100 bytes.
func ArchiveNameProblem(rel string) string {
	if len(rel) > maxArchiveName {
		return fmt.Sprintf("%s: the file name is %d bytes, at most %d travel; shorten the path", rel, len(rel), maxArchiveName)
	}
	for _, r := range rel {
		if r > 0x7e || r < 0x20 {
			return fmt.Sprintf("%q: the file name must be ASCII; rename the file", rel)
		}
	}
	return ""
}

// archiveNameProblems are the problems of every file name of a package folder, walked as Pack walks it.
func archiveNameProblems(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if msg := ArchiveNameProblem(filepath.ToSlash(rel)); msg != "" {
			out = append(out, msg)
		}
		return nil
	})
	return out
}

// Pack makes the archive of a package folder, with the files copyPackage would copy, and its SHA-256 as hex.
func Pack(dir string) ([]byte, string, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	var files int
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			return err
		}
		files++
		if files > maxPackageFiles {
			return fmt.Errorf("%w: more than %d files", ErrArchive, maxPackageFiles)
		}
		if msg := ArchiveNameProblem(filepath.ToSlash(rel)); msg != "" {
			return fmt.Errorf("%w: %s", ErrArchive, msg)
		}
		hdr := &tar.Header{Name: filepath.ToSlash(rel), Mode: int64(info.Mode().Perm()), Size: info.Size(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, cerr := io.Copy(tw, f)
		return errors.Join(cerr, f.Close())
	})
	if err != nil {
		return nil, "", err
	}
	if err := errors.Join(tw.Close(), gz.Close()); err != nil {
		return nil, "", err
	}
	if buf.Len() > MaxArchiveBytes {
		return nil, "", fmt.Errorf("%w: %d bytes packed, at most %d travel", ErrArchive, buf.Len(), MaxArchiveBytes)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:]), nil
}

// ArchiveSum is the SHA-256 of archive bytes as hex.
func ArchiveSum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Unpack writes the archive into dst (a new folder). Nothing outside dst is touched, whatever the names say.
func Unpack(raw []byte, dst string) error {
	if len(raw) > MaxArchiveBytes {
		return fmt.Errorf("%w: too large", ErrArchive)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrArchive, err)
	}
	defer gz.Close()
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var files int
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrArchive, err)
		}
		name := hdr.Name
		if name == "" || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") ||
			path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("%w: the name %q leaves the folder", ErrArchive, name)
		}
		// A PAX or GNU header (long or non-ASCII names, extensions) is not read: the platform's hub refuses it too.
		if hdr.Format&(tar.FormatPAX|tar.FormatGNU) != 0 || len(hdr.PAXRecords) > 0 {
			return fmt.Errorf("%w: %q has a PAX or GNU header", ErrArchive, name)
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("%w: %q is not a plain file", ErrArchive, name)
		}
		files++
		total += hdr.Size
		if files > maxPackageFiles || total > MaxPackageBytes || hdr.Size < 0 {
			return fmt.Errorf("%w: too many or too large files", ErrArchive)
		}
		out := filepath.Join(dst, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(hdr.Mode&0o777)|0o600)
		if err != nil {
			return err
		}
		_, cerr := io.Copy(f, io.LimitReader(tr, hdr.Size))
		if err := errors.Join(cerr, f.Close()); err != nil {
			return err
		}
	}
}
