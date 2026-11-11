package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// The replicas of a hub tell each other small things over Postgres LISTEN/NOTIFY, on one channel per schema: two hubs
// that keep their tables in two schemas of one database do not hear each other.

const notifyChannelPrefix = "hub_bus_"

// NotifyMax is the longest payload Notify takes; Postgres refuses 8000 bytes and more.
const NotifyMax = 7900

// ErrNoListen says the database cannot carry notifications to this process: a SQLite file has one process, and a
// pooler in transaction mode gives no connection that stays (set a direct HUB_DB_MIGRATE_URL beside it).
var ErrNoListen = errors.New("the database carries no notifications")

// CanListen says whether Listen can work on this database.
func (db *DB) CanListen() bool {
	return db.driver == Postgres && db.listenURL != ""
}

// Notify sends payload to every process that listens on this database's schema, this one included.
func (db *DB) Notify(ctx context.Context, payload string) error {
	if db.driver != Postgres {
		return ErrNoListen
	}
	if len(payload) > NotifyMax {
		return errors.New("notification too long")
	}
	_, err := db.sql.ExecContext(ctx, `SELECT pg_notify($1 || current_schema(), $2)`, notifyChannelPrefix, payload)
	return err
}

// Listen hands every notification of the schema to fn, in the order they came, until ctx ends. The connection is
// made again when it breaks; up is called each time it starts to listen, since what was sent while nobody listened is
// gone. It returns ErrNoListen at once when the database cannot carry notifications.
func (db *DB) Listen(ctx context.Context, up func(), fn func(payload string), failed func(error)) error {
	if !db.CanListen() {
		return ErrNoListen
	}
	for ctx.Err() == nil {
		if err := db.listenOnce(ctx, up, fn); err != nil && ctx.Err() == nil && failed != nil {
			failed(err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

func (db *DB) listenOnce(ctx context.Context, up func(), fn func(payload string)) error {
	cfg, err := pgx.ParseConfig(db.listenURL)
	if err != nil {
		return redactURLErr(err)
	}
	if db.opts.Schema != "" {
		if cfg.RuntimeParams == nil {
			cfg.RuntimeParams = map[string]string{}
		}
		cfg.RuntimeParams["search_path"] = db.opts.Schema
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	var schema string
	if err := conn.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{notifyChannelPrefix + schema}.Sanitize()); err != nil {
		return err
	}
	if up != nil {
		up()
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		fn(n.Payload)
	}
}
