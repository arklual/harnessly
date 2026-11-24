package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const managedGitAuthor = "git-author.config"

func gitQuoted(s string) (string, error) {
	if len(s) > 1024 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", errors.New("invalid Git configuration value")
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`, nil
}

// A configured system identity also precedes a generated default.
func systemGitAuthor(key string) (bool, error) {
	if os.Getenv("GIT_CONFIG_NOSYSTEM") == "1" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "git", "config", "--system", "--includes", "--get", key).Run()
	var miss *exec.ExitError
	if errors.As(err, &miss) && miss.ExitCode() == 1 {
		return false, nil
	}
	return err == nil, err
}

func configPrefix(path string, size int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(size)))
}

// A fresh XDG config includes this link's private defaults. Git reads it before
// ~/.gitconfig. An existing XDG config belongs to the person and is left alone.
func syncGitAuthor(home, xdg, linkDir string, author hubproto.GitAuthor) error {
	managed := filepath.Join(linkDir, managedGitAuthor)
	clear := func() error {
		err := os.Remove(managed)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if author.Email == "" || os.Getenv("GIT_CONFIG_GLOBAL") != "" {
		return clear()
	}
	if !filepath.IsAbs(home) || !filepath.IsAbs(linkDir) {
		return errors.New("missing absolute Git setup path")
	}
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(xdg) {
		return errors.New("invalid Git configuration directory")
	}
	nameSet, err := systemGitAuthor("user.name")
	if err != nil {
		return errors.Join(err, clear())
	}
	emailSet, err := systemGitAuthor("user.email")
	if err != nil {
		return errors.Join(err, clear())
	}
	if nameSet && emailSet {
		return clear()
	}
	name, err := gitQuoted(author.Name)
	if err != nil {
		return errors.Join(err, clear())
	}
	email, err := gitQuoted(author.Email)
	if err != nil {
		return errors.Join(err, clear())
	}
	inc, err := gitQuoted(managed)
	if err != nil {
		return err
	}
	header := []byte("[include]\n\tpath = " + inc + "\n")
	dir := filepath.Join(xdg, "git")
	cfg := filepath.Join(dir, "config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	// Git may be creating the same file. Publish a complete include atomically,
	// only if the path is vacant, even when two machines enroll under one account.
	raw, err := configPrefix(cfg, len(header))
	if errors.Is(err, os.ErrNotExist) {
		tmp, err := os.CreateTemp(dir, ".author-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.Write(header); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := os.Link(tmp.Name(), cfg); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		raw, err = configPrefix(cfg, len(header))
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !bytes.HasPrefix(raw, header) {
		return clear()
	}
	content := "[user]\n"
	if !emailSet {
		content += "\temail = " + email + "\n"
	}
	if author.Name != "" && !nameSet {
		content += "\tname = " + name + "\n"
	}
	if err := writeFileAtomic(managed, []byte(content)); err != nil {
		return fmt.Errorf("write managed Git author: %w", err)
	}
	return nil
}
