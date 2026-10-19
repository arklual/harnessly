package limits

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	watchDebounce = 500 * time.Millisecond
	watchRetry    = 5 * time.Second
)

// Watch reports the file's content every time its top-level `ts` changes: once at the start, then after writes settle.
// A directory that is missing or a watch that fails is retried every few seconds. It returns when ctx ends.
func Watch(ctx context.Context, file string, onChange func(map[string]any)) {
	var lastTs any
	read := func() {
		raw, err := os.ReadFile(file)
		if err != nil {
			return
		}
		var data map[string]any
		if json.Unmarshal(raw, &data) != nil || !truthy(data["ts"]) || data["ts"] == lastTs {
			return
		}
		lastTs = data["ts"]
		onChange(data)
	}
	for ctx.Err() == nil {
		read()
		watchOnce(ctx, file, read)
		select {
		case <-ctx.Done():
		case <-time.After(watchRetry):
		}
	}
}

func watchOnce(ctx context.Context, file string, read func()) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer w.Close()
	if w.Add(filepath.Dir(file)) != nil {
		return
	}
	name := filepath.Base(file)
	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) == name {
				debounce = time.After(watchDebounce)
			}
		case _, ok := <-w.Errors:
			if !ok {
				return
			}
			return
		case <-debounce:
			debounce = nil
			read()
		}
	}
}
