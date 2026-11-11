package apps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ly-solutions/harnessly/internal/journal"
)

// The journal of the commands of app windows. A call of a window's tool is a command with a life of its own:
// it is written to the disk before the page may take it, handed to the page until the page says it has it, and ended
// once, by the window's answer or by a refusal. What was memory only (a call taken once, lost with a restart of the
// runtime or a reload of the page) is a row here, so a restarted runtime goes on delivering what is not over, and the
// agent's call, repeated by its proxy with the same key (internal/appmcp), finds its command and not a second one.
//
//	queued → sent → acked → done | failed
//	   any of the three → rejected | expired | canceled
//
// done is set only by the window's answer with a result; failed by its answer with an error. rejected: the window was
// on another screen than the call was made for, or offers the tool no more. expired: the term ran out. canceled: the
// caller left. A command that is over stays over: the page that asks about it is told it is dead, and what it answers
// to it afterwards is dropped, so a command the agent was told failed is never run later.

// The statuses of a command.
const (
	CommandQueued   = "queued"
	CommandSent     = "sent"
	CommandAcked    = "acked"
	CommandDone     = "done"
	CommandFailed   = "failed"
	CommandRejected = "rejected"
	CommandExpired  = "expired"
	CommandCanceled = "canceled"
)

const (
	// PageCallResend is how long a command the page took and did not acknowledge waits before it is handed out again.
	PageCallResend = 5 * time.Second
	// PageCallReack is the same for a command the page acknowledged and has not answered: its page may have been
	// reloaded. The page that still runs it ignores the repeat, and a window that ran it answers what it answered.
	PageCallReack = 15 * time.Second
	// PageGone is how long a page that took a command must be silent before another page of the chat is given it.
	PageGone = 5 * time.Second
	// PageCallOrphan is how long a command nobody waits for is kept for its caller to come back with the same key (the
	// proxy repeats a call it lost at once). Until the caller is back the command is not handed to a page; after this
	// long it is canceled: an agent that was stopped, or was told its call was lost, does not have it run later.
	PageCallOrphan = 10 * time.Second
	// PageCommandsKept is how long a command that is over is remembered: for the proxy's repeat of the call, and for a
	// page that still holds it.
	PageCommandsKept = 10 * time.Minute
	// PageCommandsFile is the journal's file in the runtime's data folder.
	PageCommandsFile = "page-commands.db"
)

// CommandOver says whether a status is a final one.
func CommandOver(status string) bool {
	switch status {
	case CommandDone, CommandFailed, CommandRejected, CommandExpired, CommandCanceled:
		return true
	}
	return false
}

// PageCommand is one row of the journal.
type PageCommand struct {
	ID   string
	Conv string
	App  string
	Tool string
	Args json.RawMessage
	// Key is the caller's idempotency key of the call, "" when it gave none.
	Key    string
	Status string
	// The condition: the window's screen and the hash of its tools when the call was made, "" when not known.
	Screen string
	Offer  string
	// Result is the window's result (done); Error its error (failed) or the words of the refusal.
	Result json.RawMessage
	Error  string
	// Times in milliseconds; 0 when it has not happened.
	Created, Sent, Acked, Ended, Expires int64
	// Deliveries is how many times it was handed to a page, Page the page that has it, Legacy whether that page does
	// not acknowledge (an older page: it gets a command once).
	Deliveries int
	Page       string
	Legacy     bool
}

// commandLog is the journal's file.
type commandLog struct{ db *sql.DB }

const commandSchema = `CREATE TABLE IF NOT EXISTS page_commands (
	id TEXT PRIMARY KEY, conv TEXT NOT NULL, app TEXT NOT NULL, tool TEXT NOT NULL, args BLOB, key TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL, screen TEXT NOT NULL DEFAULT '', offer TEXT NOT NULL DEFAULT '', result BLOB, error TEXT NOT NULL DEFAULT '',
	created INTEGER NOT NULL, sent INTEGER NOT NULL DEFAULT 0, acked INTEGER NOT NULL DEFAULT 0, ended INTEGER NOT NULL DEFAULT 0,
	expires INTEGER NOT NULL, deliveries INTEGER NOT NULL DEFAULT 0, page TEXT NOT NULL DEFAULT '', legacy INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS page_commands_ended ON page_commands (ended);`

func openCommandLog(path string) (*commandLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// The journal holds what agents asked windows to do, arguments and answers: it is the owner's alone from its first
	// byte. SQLite gives its -wal and -shm files the mode of the file they belong to.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	// One writer: the journal is written under the page tools' lock.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(commandSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, side := range []string{"-wal", "-shm"} {
		if err := os.Chmod(path+side, 0o600); err != nil && !os.IsNotExist(err) {
			_ = db.Close()
			return nil, err
		}
	}
	return &commandLog{db: db}, nil
}

func (l *commandLog) save(c *PageCommand) error {
	legacy := 0
	if c.Legacy {
		legacy = 1
	}
	_, err := l.db.Exec(`INSERT INTO page_commands (id, conv, app, tool, args, key, status, screen, offer, result, error, created, sent, acked, ended, expires, deliveries, page, legacy)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status, result=excluded.result, error=excluded.error, sent=excluded.sent,
			acked=excluded.acked, ended=excluded.ended, deliveries=excluded.deliveries, page=excluded.page, legacy=excluded.legacy`,
		c.ID, c.Conv, c.App, c.Tool, []byte(c.Args), c.Key, c.Status, c.Screen, c.Offer, []byte(c.Result), c.Error,
		c.Created, c.Sent, c.Acked, c.Ended, c.Expires, c.Deliveries, c.Page, legacy)
	return err
}

// load reads the commands that are not over and those that ended after since; older ones are deleted.
func (l *commandLog) load(since int64) ([]*PageCommand, error) {
	if _, err := l.db.Exec(`DELETE FROM page_commands WHERE ended > 0 AND ended < ?`, since); err != nil {
		return nil, err
	}
	rows, err := l.db.Query(`SELECT id, conv, app, tool, args, key, status, screen, offer, result, error, created, sent, acked, ended, expires, deliveries, page, legacy
		FROM page_commands ORDER BY created, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PageCommand
	for rows.Next() {
		var c PageCommand
		var args, result []byte
		var legacy int
		if err := rows.Scan(&c.ID, &c.Conv, &c.App, &c.Tool, &args, &c.Key, &c.Status, &c.Screen, &c.Offer, &result, &c.Error,
			&c.Created, &c.Sent, &c.Acked, &c.Ended, &c.Expires, &c.Deliveries, &c.Page, &legacy); err != nil {
			return nil, err
		}
		c.Args, c.Result, c.Legacy = args, result, legacy == 1
		out = append(out, &c)
	}
	return out, rows.Err()
}

func (l *commandLog) prune(before int64) {
	_, _ = l.db.Exec(`DELETE FROM page_commands WHERE ended > 0 AND ended < ?`, before)
}

func (l *commandLog) close() error { return l.db.Close() }

// callKey is the idempotency key the caller's proxy gives a tool call, and whether this request repeats one whose
// answer the proxy lost.
type callKey struct {
	key   string
	again bool
}

type callKeyCtx struct{}

// WithCallKey marks a tool call's context with the caller's idempotency key (internal/appmcp sends it); again says the
// request repeats a call whose answer was lost with the connection.
func WithCallKey(ctx context.Context, key string, again bool) context.Context {
	if key == "" || len(key) > 128 {
		return ctx
	}
	return context.WithValue(ctx, callKeyCtx{}, callKey{key: key, again: again})
}

func callKeyOf(ctx context.Context) callKey {
	k, _ := ctx.Value(callKeyCtx{}).(callKey)
	return k
}

// CallRepeats says whether the tool call of ctx repeats one whose answer its caller lost.
func CallRepeats(ctx context.Context) bool { return callKeyOf(ctx).again }

// LostCall is what a repeated call is answered when nothing remembers the first one: a tool of the host that is not a
// command of a window runs once, and whether it ran before the runtime restarted cannot be told.
const LostCall = "harnessly's runtime restarted while this call was running, so its answer is lost and the call may or " +
	"may not have been done. Check what it changed before repeating it."

// ErrPageCallRejected: the command was not run because its condition did not hold; the error's text says what the
// window shows now.
type ErrPageCallRejected struct{ Why string }

func (e ErrPageCallRejected) Error() string { return e.Why }

// ErrPageCallDead: the command is over, and a page's word about it comes too late.
var ErrPageCallDead = errors.New("apps: the command is over")

// logCommand writes a command's change of status to the machine's journal (`harnessly logs`).
func logCommand(c *PageCommand, from string, note string) {
	level := journal.Info
	switch c.Status {
	case CommandFailed, CommandRejected, CommandExpired:
		level = journal.Warn
	}
	text := "command " + c.ID + ": " + from + " → " + c.Status
	if from == "" {
		text = "command " + c.ID + ": " + c.Status
	}
	if note != "" {
		text += " (" + note + ")"
	}
	e := journal.Entry{Level: level, Source: JournalSource(c.App), Event: "app.command", Text: text, Chat: c.Conv, App: c.App, Tool: c.Tool}
	if c.Ended > 0 {
		e.Dur = c.Ended - c.Created
	}
	journal.Add(e)
}

func ms(t time.Time) int64 { return t.UnixMilli() }
