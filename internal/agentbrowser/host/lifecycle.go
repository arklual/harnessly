package host

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

func profileLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "host.lock"), os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// Running checks the host's advisory lock rather than trusting stale PID files.
func Running(dir string) bool {
	f, err := profileLock(dir)
	if err == nil {
		_ = f.Close()
		return false
	}
	return errors.Is(err, syscall.EWOULDBLOCK)
}

// Stop gracefully closes only the browser behind this profile's private relay.
// Waiting for the host lock also waits for Chrome's final cookie/session writes.
func Stop(ctx context.Context, dir, socket string) error {
	lock, err := profileLock(dir)
	if err == nil {
		_ = lock.Close()
		return nil
	}
	if os.IsNotExist(err) {
		return nil
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		return err
	}
	netConn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	conn := cdp.New(cdp.NewStream(netConn), nil)
	defer conn.Close()
	go func() { _ = conn.Run(ctx) }()
	_, callErr := conn.Call(ctx, "Browser.close", nil, "")
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		lock, err := profileLock(dir)
		if err == nil {
			_ = lock.Close()
			return nil
		}
		if os.IsNotExist(err) {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), callErr)
		case <-ticker.C:
		}
	}
}
