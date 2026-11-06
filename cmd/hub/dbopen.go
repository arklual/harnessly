package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// openDB opens the hub database with the HUB_DB_* settings.
func openDB(ctx context.Context, cfg *config.Config, log *slog.Logger) (*store.DB, error) {
	return store.OpenWith(ctx, cfg.DatabaseURL, dbOptions(cfg, log))
}

func dbOptions(cfg *config.Config, log *slog.Logger) store.Options {
	return store.Options{
		Schema:     cfg.DBSchema,
		Pooler:     cfg.DBPooler,
		MigrateURL: cfg.DBMigrateURL,
		Check:      cfg.DBCheck,
		Log:        log,
	}
}

// connectDB connects to the hub database with the HUB_DB_* settings without migrating it.
func connectDB(ctx context.Context, cfg *config.Config, log *slog.Logger) (*store.DB, error) {
	return store.ConnectWith(ctx, cfg.DatabaseURL, dbOptions(cfg, log))
}

// exitCode is the process exit code of a failed command: the code of a startup refusal, else 1.
func exitCode(err error) int {
	var se *store.StartupError
	if errors.As(err, &se) {
		return se.ExitCode()
	}
	return 1
}
