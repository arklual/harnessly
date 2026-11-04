package agentbrowser

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	profileSizeTTL     = time.Minute
	profileSizeRetry   = 30 * time.Second
	profileSizeLimit   = 256
	profileSizeWorkers = 2
)

type profileSizeEntry struct {
	size    *int64
	until   time.Time
	pending bool
}

type profileSizes struct {
	mu        sync.Mutex
	entries   map[string]profileSizeEntry
	running   int
	calculate func(context.Context, string) (int64, error)
}

// SizeBytes returns cached managed Chrome file sizes, or nil while unknown.
// Only background workers traverse disk; repeated failures wait before retrying.
func (s *ProfileStore) SizeBytes(id string) *int64 {
	p, ok := s.Lookup(id)
	if !ok || p.Kind != "managed" || !profileID.MatchString(id) {
		return nil
	}
	return s.sizes.get(id, func(ctx context.Context, id string) (int64, error) {
		return profileDirectorySize(ctx, filepath.Dir(filepath.Dir(s.path)), id)
	})
}

func (c *profileSizes) get(id string, calculate func(context.Context, string) (int64, error)) *int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	entry := c.entries[id]
	if entry.pending || now.Before(entry.until) {
		return copySize(entry.size)
	}
	if c.running >= profileSizeWorkers {
		return nil
	}
	if c.entries == nil {
		c.entries = make(map[string]profileSizeEntry)
	}
	if len(c.entries) >= profileSizeLimit {
		oldest := ""
		for key, value := range c.entries {
			if !value.pending && (oldest == "" || value.until.Before(c.entries[oldest].until)) {
				oldest = key
			}
		}
		if oldest == "" {
			return nil
		}
		delete(c.entries, oldest)
	}
	c.entries[id] = profileSizeEntry{pending: true}
	c.running++
	if c.calculate != nil {
		calculate = c.calculate
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		size, err := calculate(ctx, id)
		result := profileSizeEntry{until: time.Now().Add(profileSizeRetry)}
		if err == nil {
			result.size = &size
			result.until = time.Now().Add(profileSizeTTL)
		}
		c.mu.Lock()
		c.entries[id] = result
		c.running--
		c.mu.Unlock()
	}()
	return nil
}

func copySize(size *int64) *int64 {
	if size == nil {
		return nil
	}
	value := *size
	return &value
}

// Open every component relative to its parent descriptor, refusing symlinks even
// if Chrome or an import replaces an entry during traversal.
func profileDirectorySize(ctx context.Context, dataDir, id string) (int64, error) {
	if !profileID.MatchString(id) {
		return 0, errors.New("invalid browser profile id")
	}
	dir, err := os.Open(dataDir)
	if err != nil {
		return 0, err
	}
	for _, part := range []string{"agent-browser", "profiles", id, "chrome"} {
		child, openErr := openSizeDirectory(dir, part)
		_ = dir.Close()
		if openErr != nil {
			return 0, openErr
		}
		dir = child
	}
	defer dir.Close()
	return directoryFileSize(ctx, dir)
}

func openSizeDirectory(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func directoryFileSize(ctx context.Context, dir *os.File) (int64, error) {
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		entries, err := dir.ReadDir(128)
		if err != nil && err != io.EOF {
			return 0, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			var stat unix.Stat_t
			if err := unix.Fstatat(int(dir.Fd()), entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return 0, err
			}
			switch stat.Mode & unix.S_IFMT {
			case unix.S_IFREG:
				size += stat.Size
			case unix.S_IFDIR:
				child, err := openSizeDirectory(dir, entry.Name())
				if err != nil {
					return 0, err
				}
				n, err := directoryFileSize(ctx, child)
				_ = child.Close()
				if err != nil {
					return 0, err
				}
				size += n
			}
		}
		if err == io.EOF {
			return size, nil
		}
	}
}
