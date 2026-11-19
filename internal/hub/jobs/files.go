package jobs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The files of a job live on the hub's disk, in <dir>/<job id>/: input.tgz (what the builder unpacks first) and out/ (what
// it hands back). They are removed a day after the job ended. The hub holds them only so the submitter can take them
// from there; a document or a chat gets the files from the submitter's side.
const (
	// MaxInput is the biggest input archive; MaxOutput the biggest sum of a job's result files.
	MaxInput  = 1 << 30
	MaxOutput = 4 << 30
	// FilesKeep is how long a final job's files stay on the disk.
	FilesKeep = 24 * time.Hour
)

var jobID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// Files is the hub's folder of the jobs' files.
type Files struct{ Root string }

func (f Files) dir(id string) (string, error) {
	if f.Root == "" || !jobID.MatchString(id) {
		return "", errors.New("bad job id")
	}
	return filepath.Join(f.Root, id), nil
}

// HasInput says the input archive is complete (it is written under another name and renamed when whole).
func (f Files) HasInput(id string) bool {
	d, err := f.dir(id)
	if err != nil {
		return false
	}
	st, err := os.Stat(filepath.Join(d, "input.tgz"))
	return err == nil && st.Mode().IsRegular()
}

// PutInput stores the input archive: whole or not at all.
func (f Files) PutInput(id string, r io.Reader) (int64, error) {
	d, err := f.dir(id)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return 0, err
	}
	return writeAtomic(filepath.Join(d, "input.tgz"), r, MaxInput)
}

// OpenInput opens the input archive.
func (f Files) OpenInput(id string) (*os.File, int64, error) {
	d, err := f.dir(id)
	if err != nil {
		return nil, 0, err
	}
	fh, err := os.Open(filepath.Join(d, "input.tgz"))
	if err != nil {
		return nil, 0, err
	}
	st, err := fh.Stat()
	if err != nil {
		fh.Close()
		return nil, 0, err
	}
	return fh, st.Size(), nil
}

// CleanOut makes a result file's path safe: relative, no "..", no hidden folder tricks.
func CleanOut(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) {
		return "", fmt.Errorf("bad path %q", rel)
	}
	c := path.Clean(rel)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || len(c) > 512 {
		return "", fmt.Errorf("bad path %q", rel)
	}
	return c, nil
}

// PutOut stores one result file under its path.
func (f Files) PutOut(id, rel string, r io.Reader) (int64, error) {
	d, err := f.dir(id)
	if err != nil {
		return 0, err
	}
	c, err := CleanOut(rel)
	if err != nil {
		return 0, err
	}
	used := int64(0)
	for _, o := range f.ListOut(id) {
		if o.Path != c {
			used += o.Size
		}
	}
	room := MaxOutput - used
	if room <= 0 {
		return 0, errors.New("the job's result files are over the limit")
	}
	dst := filepath.Join(d, "out", filepath.FromSlash(c))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, err
	}
	return writeAtomic(dst, r, room)
}

// OpenOut opens one result file.
func (f Files) OpenOut(id, rel string) (*os.File, int64, error) {
	d, err := f.dir(id)
	if err != nil {
		return nil, 0, err
	}
	c, err := CleanOut(rel)
	if err != nil {
		return nil, 0, err
	}
	fh, err := os.Open(filepath.Join(d, "out", filepath.FromSlash(c)))
	if err != nil {
		return nil, 0, err
	}
	st, err := fh.Stat()
	if err != nil || !st.Mode().IsRegular() {
		fh.Close()
		return nil, 0, fs.ErrNotExist
	}
	return fh, st.Size(), nil
}

// ListOut lists the result files, by path.
func (f Files) ListOut(id string) []store.JobResultFile {
	d, err := f.dir(id)
	if err != nil {
		return nil
	}
	root := filepath.Join(d, "out")
	var out []store.JobResultFile
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !e.Type().IsRegular() || strings.HasSuffix(p, ".part") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if info, err := e.Info(); err == nil {
			out = append(out, store.JobResultFile{Path: filepath.ToSlash(rel), Size: info.Size()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Remove drops a job's files.
func (f Files) Remove(id string) {
	if d, err := f.dir(id); err == nil {
		_ = os.RemoveAll(d)
	}
}

// Sweep removes the folders of jobs the keep function says are over, and folders nobody knows (a job row removed).
func (f Files) Sweep(keep func(id string) bool) {
	if f.Root == "" {
		return
	}
	entries, err := os.ReadDir(f.Root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && jobID.MatchString(e.Name()) && !keep(e.Name()) {
			f.Remove(e.Name())
		}
	}
}

// writeAtomic copies r to a ".part" file and renames it when whole; more than max bytes is an error.
func writeAtomic(dst string, r io.Reader, max int64) (int64, error) {
	tmp := dst + ".part"
	fh, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(fh, io.LimitReader(r, max+1))
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = fmt.Errorf("the file is over %d bytes", max)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return n, nil
}
