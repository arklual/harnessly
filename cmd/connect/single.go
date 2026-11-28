package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var (
	singleOnce sync.Once
	singleErr  error
	singleFile *os.File // kept open: the lock lives as long as the process
)

// errRunning: one computer keeps one link; a second Connect with the same key would push the first one's link off
// the server and the two would take turns forever.
var errRunning = errors.New("Connect уже запущен на этом компьютере (служба автозапуска или другое окно): " +
	"второй экземпляр не нужен, остановите его или выключите автозапуск: harnessly-connect autostart off")

// single takes this computer's Connect lock, next to the device's key.
func single() error {
	singleOnce.Do(func() {
		p := filepath.Join(filepath.Dir(configPath()), "connect.lock")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return // no folder: nothing to guard
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return
		}
		if !lockFile(f) {
			_ = f.Close()
			singleErr = errRunning
			return
		}
		singleFile = f
	})
	return singleErr
}
