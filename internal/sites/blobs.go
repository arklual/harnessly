package sites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrBlobNotFound is what Open returns for a hash the store does not hold.
var ErrBlobNotFound = errors.New("blob not found")

// Blobs holds the content of files by the sha256 of that content. A blob is immutable: putting the same hash twice is
// the same file.
type Blobs interface {
	Has(ctx context.Context, hash string) (bool, error)
	// Put stores exactly size bytes read from r under hash. The caller has checked that they hash to it.
	Put(ctx context.Context, hash string, r io.Reader, size int64) error
	// Open returns the content and its size; ErrBlobNotFound when there is none.
	Open(ctx context.Context, hash string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, hash string) error
}

// FolderBlobs keeps blobs as files in a folder (the self-hosted default): <root>/ab/abcdef….
type FolderBlobs struct{ Root string }

// NewFolderBlobs makes the folder store, creating the folder.
func NewFolderBlobs(root string) (*FolderBlobs, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &FolderBlobs{Root: root}, nil
}

func (f *FolderBlobs) path(hash string) (string, error) {
	if !ValidHash(hash) {
		return "", fmt.Errorf("blob %q: not a sha256", hash)
	}
	return filepath.Join(f.Root, hash[:2], hash), nil
}

func (f *FolderBlobs) Has(_ context.Context, hash string) (bool, error) {
	p, err := f.path(hash)
	if err != nil {
		return false, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil && st.Mode().IsRegular(), err
}

func (f *FolderBlobs) Put(_ context.Context, hash string, r io.Reader, size int64) error {
	p, err := f.path(hash)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(r, size+1))
	if err == nil && n != size {
		err = fmt.Errorf("blob %s: %d bytes, expected %d", hash, n, size)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	// Atomic: a reader sees the whole file or none of it.
	return os.Rename(tmp.Name(), p)
}

func (f *FolderBlobs) Open(_ context.Context, hash string) (io.ReadCloser, int64, error) {
	p, err := f.path(hash)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, ErrBlobNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	st, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}
	return file, st.Size(), nil
}

func (f *FolderBlobs) Delete(_ context.Context, hash string) error {
	p, err := f.path(hash)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// HashReader hashes what is read through it.
type HashReader struct {
	r io.Reader
	h interface {
		io.Writer
		Sum([]byte) []byte
	}
	N int64
}

// NewHashReader wraps r.
func NewHashReader(r io.Reader) *HashReader { return &HashReader{r: r, h: sha256.New()} }

func (h *HashReader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	if n > 0 {
		_, _ = h.h.Write(p[:n]) // a hash never fails to take bytes
		h.N += int64(n)
	}
	return n, err
}

// Sum is the hex sha256 of what has been read.
func (h *HashReader) Sum() string { return hex.EncodeToString(h.h.Sum(nil)) }
