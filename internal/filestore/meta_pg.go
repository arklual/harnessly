package filestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const pgChannel = "design_sync"

// pgPollEvery is how often a replica looks at the revision counters when no notification came: the fallback for a
// broken LISTEN connection, a proxy that drops notifications (PgBouncer in transaction mode), or a missed message.
var pgPollEvery = 3 * time.Second

// pgPollHeardEvery is the same look while the LISTEN connection is up and has not missed a change: notifications
// wake the waiters, and the counters are read only to catch one that got lost.
var pgPollHeardEvery = 3 * time.Minute

// pgMeta keeps the metadata in Postgres: a schema per room (so every room's queries are the same as SQLite's) and a
// registry of rooms in the default schema. Any number of hub replicas may share it.
type pgMeta struct {
	raw      string
	admin    *sql.DB
	noListen bool
	// registry is the table of rooms, schema the prefix of a room's schema: ds_rooms and r_ in a design-sync
	// database, led by the store's prefix in a database shared with other tables.
	registry, schema string

	mu        sync.Mutex
	rooms     map[string]*sqlRoom
	notifiers map[string]*notifier
	lastRev   map[string]int64
	// listening: the LISTEN connection is up. heard counts the notifications of each room, seenHeard is that count
	// at the last look. deaf: a room's revision moved with no notification for it, so this connection does not
	// deliver them and the counters are read often until it is made again.
	listening bool
	deaf      bool
	heard     map[string]int64
	seenHeard map[string]int64

	stop context.CancelFunc
	wg   sync.WaitGroup
}

func openPostgresMetaOpts(ctx context.Context, raw string, noListen bool, prefix string) (*pgMeta, error) {
	if !validPrefix(prefix) {
		return nil, fmt.Errorf("db prefix %q: lowercase letters, digits and underscores", prefix)
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		return nil, fmt.Errorf("db %s: %w", redact(raw), err)
	}
	admin.SetMaxOpenConns(8)
	admin.SetConnMaxIdleTime(time.Minute)
	if err := admin.PingContext(ctx); err != nil {
		admin.Close()
		return nil, fmt.Errorf("db %s: %w", redact(raw), err)
	}
	m := &pgMeta{raw: raw, admin: admin, rooms: map[string]*sqlRoom{}, notifiers: map[string]*notifier{}, lastRev: map[string]int64{}, noListen: noListen,
		heard: map[string]int64{}, seenHeard: map[string]int64{},
		registry: "ds_rooms", schema: "r_"}
	if prefix != "" {
		m.registry, m.schema = prefix+"rooms", prefix+"r_"
	}
	if err := m.ddl(ctx, admin, `CREATE TABLE IF NOT EXISTS `+m.registry+` (room TEXT PRIMARY KEY, id BIGSERIAL UNIQUE, at BIGINT NOT NULL);`); err != nil {
		admin.Close()
		return nil, err
	}
	bg, stop := context.WithCancel(context.Background())
	m.stop = stop
	m.wg.Add(1)
	go m.poll(bg)
	if !noListen {
		m.wg.Add(1)
		go m.listen(bg)
	}
	return m, nil
}

// ddl runs schema statements under one cluster-wide lock: replicas starting together must not race on the catalog.
func (m *pgMeta) ddl(ctx context.Context, db *sql.DB, stmts string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('design-sync-ddl', 0))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, stmts); err != nil {
		return err
	}
	return tx.Commit()
}

// schemaOf registers a room (once) and returns the name of its schema, r_<serial> (led by the prefix). The serial comes from the
// registry, so two rooms can never share a schema whatever their names look like. Created under the DDL lock.
func (m *pgMeta) schemaOf(ctx context.Context, room string) (string, error) {
	tx, err := m.admin.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('design-sync-ddl', 0))`); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+m.registry+` (room, at) VALUES ($1, $2) ON CONFLICT (room) DO NOTHING`, room, time.Now().UnixMilli()); err != nil {
		return "", err
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM `+m.registry+` WHERE room = $1`, room).Scan(&id); err != nil {
		return "", err
	}
	schema := fmt.Sprintf("%s%d", m.schema, id)
	if _, err := tx.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS "`+schema+`"`); err != nil {
		return "", err
	}
	return schema, tx.Commit()
}

func (m *pgMeta) notifierFor(room string) *notifier {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.notifierLocked(room)
}

func (m *pgMeta) notifierLocked(room string) *notifier {
	n, ok := m.notifiers[room]
	if !ok {
		n = newNotifier()
		m.notifiers[room] = n
	}
	return n
}

func (m *pgMeta) Room(ctx context.Context, room string) (RoomMeta, error) {
	if room != DefaultRoom && !ValidRoom(room) {
		return nil, fmt.Errorf("room %q: %w", room, errInvalidRoom)
	}
	m.mu.Lock()
	if r, ok := m.rooms[room]; ok {
		m.mu.Unlock()
		return r, nil
	}
	m.mu.Unlock()

	schema, err := m.schemaOf(ctx, room)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(m.raw)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(time.Minute)
	if err := m.ddl(ctx, db, roomSchema); err != nil {
		db.Close()
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rooms[room]; ok { // another request opened it meanwhile
		db.Close()
		return r, nil
	}
	r := &sqlRoom{db: db, d: dialect{pg: true}, n: m.notifierLocked(room), mu: newChanLock(false),
		notifyPG: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `SELECT pg_notify($1, $2)`, pgChannel, room)
			return err
		}}
	m.rooms[room] = r
	return r, nil
}

func (m *pgMeta) Rooms(ctx context.Context) ([]string, error) {
	rows, err := m.admin.QueryContext(ctx, `SELECT room FROM `+m.registry+` ORDER BY room`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TryLock is a session advisory lock on a connection kept for as long as the lock is held.
func (m *pgMeta) TryLock(ctx context.Context, name string) (func(), bool, error) {
	conn, err := m.admin.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, "design-sync-"+name).Scan(&got); err != nil {
		conn.Close()
		return nil, false, err
	}
	if !got {
		conn.Close()
		return nil, false, nil
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, "design-sync-"+name)
		conn.Close()
	}, true, nil
}

// listen turns NOTIFY from any replica into wakes of this process's long-polls.
func (m *pgMeta) listen(ctx context.Context) {
	defer m.wg.Done()
	for ctx.Err() == nil {
		if err := m.listenOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("postgres LISTEN lost, polling until it is back", "err", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (m *pgMeta) listenOnce(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, m.raw)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+pgChannel); err != nil {
		return err
	}
	m.setListening(true)
	defer m.setListening(false)
	m.wakeAll() // whatever was written while we were not listening
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.heard[n.Payload]++
		m.mu.Unlock()
		m.notifierFor(n.Payload).Broadcast()
	}
}

// setListening notes the LISTEN connection coming up or going away; a new connection is trusted again.
func (m *pgMeta) setListening(on bool) {
	m.mu.Lock()
	m.listening, m.deaf = on, false
	m.mu.Unlock()
}

// pollWait is the wait before the next look at the revision counters.
func (m *pgMeta) pollWait() time.Duration {
	m.mu.Lock()
	trusted := m.listening && !m.deaf
	m.mu.Unlock()
	if !trusted || pgPollHeardEvery <= pgPollEvery {
		return pgPollEvery
	}
	return pgPollHeardEvery + rand.N(pgPollHeardEvery/2+1)
}

// noteRev records a room's newest revision as the poll read it and answers whether it moved. A move of a room
// looked at before, with no notification for it since, marks the LISTEN connection as one that loses them.
func (m *pgMeta) noteRev(room string, top int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	last, known := m.lastRev[room]
	moved := top != last
	m.lastRev[room] = top
	heard := m.heard[room]
	if moved && known && m.listening && heard == m.seenHeard[room] {
		m.deaf = true
	}
	m.seenHeard[room] = heard
	return moved
}

func (m *pgMeta) wakeAll() {
	m.mu.Lock()
	ns := make([]*notifier, 0, len(m.notifiers))
	for _, n := range m.notifiers {
		ns = append(ns, n)
	}
	m.mu.Unlock()
	for _, n := range ns {
		n.Broadcast()
	}
}

// poll wakes a room's waiters when its newest revision moved without a notification reaching us.
func (m *pgMeta) poll(ctx context.Context) {
	defer m.wg.Done()
	t := time.NewTimer(m.pollWait())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		t.Reset(m.pollWait())
		m.mu.Lock()
		rooms := make(map[string]*sqlRoom, len(m.rooms))
		for k, v := range m.rooms {
			rooms[k] = v
		}
		m.mu.Unlock()
		for name, r := range rooms {
			var top int64
			if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(rev), 0) FROM history`).Scan(&top); err != nil {
				continue
			}
			if m.noteRev(name, top) {
				r.n.Broadcast()
			}
		}
	}
}

func (m *pgMeta) Close() error {
	m.stop()
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rooms {
		r.db.Close()
	}
	return m.admin.Close()
}

// validPrefix: a prefix that cannot break out of an identifier.
func validPrefix(p string) bool {
	if len(p) > 20 {
		return false
	}
	for _, c := range p {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
