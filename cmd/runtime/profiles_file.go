package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/providers"
)

// loadExtraProfiles adds the model profiles of a file (RUNTIME_PROFILES_FILE) and of an adapter
// (RUNTIME_PROFILES_ADAPTER_URL, token in RUNTIME_PROFILES_ADAPTER_TOKEN_FILE) to the built-in presets, and gives the
// store the command that reads the secrets the profiles name (RUNTIME_PROFILES_SECRET_CMD, «{name}» is the secret's
// name, the value is what the command prints). With none of the settings nothing changes. A bad file or a silent adapter
// is told on stderr and adds nothing: the built-in presets keep working.
func loadExtraProfiles(store *providers.Store) {
	store.Secrets = providers.CommandSecrets(os.Getenv("RUNTIME_PROFILES_SECRET_CMD"))
	if path := os.Getenv("RUNTIME_PROFILES_FILE"); path != "" {
		if err := providers.LoadProfileFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "runtime: profiles file %s not loaded: %v\n", path, err)
		}
	}
	if url := os.Getenv("RUNTIME_PROFILES_ADAPTER_URL"); url != "" {
		c, err := ext.NewClient(ext.Config{BaseURL: url, TokenFile: os.Getenv("RUNTIME_PROFILES_ADAPTER_TOKEN_FILE")})
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = providers.LoadAdapter(ctx, c)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "runtime: profiles adapter not loaded: %v\n", err)
		}
	}
}
