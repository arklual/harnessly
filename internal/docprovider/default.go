package docprovider

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/integrations"
)

var (
	loadOnce sync.Once
	adapters []*ExtProvider
	pinned   Provider
)

// Pin makes For answer p in this process until the returned function is called: the seam a test or an embedding
// program uses instead of a declared adapter file.
func Pin(p Provider) (restore func()) {
	was := pinned
	pinned = p
	return func() { pinned = was }
}

// Declared says whether a provider other than the built-in one is in use (an adapter of the file, or a pinned one).
func Declared() bool { return pinned != nil || len(Adapters()) > 0 }

// Adapters are the documents adapters the file RUNTIME_DOC_ADAPTERS_FILE declares, loaded once per process. Without the
// variable there are none and the documents are the built-in catalog, as they always were. An adapter that does not
// answer is told on stderr; the built-in catalog goes on.
func Adapters() []*ExtProvider {
	loadOnce.Do(func() {
		path := os.Getenv("RUNTIME_DOC_ADAPTERS_FILE")
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

// For is the provider of a runtime: the declared adapter (the first of the file) when there is one, the built-in
// catalog otherwise.
func For(cfg integrations.Config, store *integrations.Threads) Provider {
	if pinned != nil {
		return pinned
	}
	if a := Adapters(); len(a) > 0 {
		return a[0]
	}
	return Builtin{Cfg: cfg, Store: store}
}
