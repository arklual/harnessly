package memstore

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	loadOnce sync.Once
	adapters []*ExtStore
)

// Adapters are the memory adapters the file RUNTIME_MEMORY_ADAPTERS_FILE declares, loaded once per process. Without
// the variable there are none and the memory is the files, as it always was. An adapter that does not answer is told
// on stderr; the files go on.
func Adapters() []*ExtStore {
	loadOnce.Do(func() {
		path := os.Getenv("RUNTIME_MEMORY_ADAPTERS_FILE")
		if path == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var errs []error
		adapters, errs = LoadAdapters(ctx, path)
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "runtime: %v\n", err)
		}
	})
	return adapters
}

// For is the store of a runtime: the files, with the first declared adapter over them when there is one.
func For(files FileStore) Store {
	if a := Adapters(); len(a) > 0 {
		return Layered{Top: a[0], Base: files}
	}
	return files
}
