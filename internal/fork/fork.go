// Package fork cuts an agent's transcript at one of the person's messages and writes what comes before it as a new
// session of the agent, which the agent CLI resumes as its own: the conversation goes on from that point, the old
// transcript stays as it was.
//
// Neither CLI can be driven there by a flag of an interactive start: Claude Code's --resume-session-at is read only in
// print mode, and its Esc-Esc picker (like Codex's Esc-Esc backtrack) rewinds inside the same transcript, which then
// holds both branches one after the other. A new session file holds only the kept one, and `claude --resume <id>` /
// `codex resume <id>` take it as it is (the session id inside the lines is the file's own, a uuid of the same length,
// so every kept line stays at its byte offset and the feed's entry ids do not move).
package fork

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Providers, as the conversations name them.
const (
	Claude = "claude"
	Codex  = "codex"
)

var (
	// ErrNotFound: the transcript holds no line of that message.
	ErrNotFound = errors.New("fork: the message is not in the transcript")
	// ErrNothingKept: the cut is before the first message, and an agent cannot resume a session without one.
	ErrNothingKept = errors.New("fork: nothing before the message")
)

// maxLine is the longest transcript line read (a pasted picture lives in one).
const maxLine = 64 << 20

type line struct {
	at   int64
	kind string
	// message: the line is a message of the conversation (a user or assistant message), not a record about it.
	message bool
	// opener: the line only opens the turn of the message after it (its queueing, the turn's context).
	opener bool
	uuid   string
}

// claudeLine reads what a cut needs of a Claude Code line.
func claudeLine(raw []byte) line {
	var o struct {
		Type string `json:"type"`
		UUID string `json:"uuid"`
	}
	_ = json.Unmarshal(raw, &o)
	return line{kind: o.Type, uuid: o.UUID, message: o.Type == "user" || o.Type == "assistant",
		opener: o.Type == "queue-operation" || o.Type == "file-history-snapshot"}
}

// codexLine reads what a cut needs of a Codex rollout line.
func codexLine(raw []byte) line {
	var o struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Role string `json:"role"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(raw, &o)
	l := line{kind: o.Type}
	switch {
	case o.Type == "response_item" && o.Payload.Type == "message":
		l.message = o.Payload.Role == "user" || o.Payload.Role == "assistant"
		l.opener = o.Payload.Role == "user"
	case o.Type == "turn_context", o.Type == "world_state":
		l.opener = true
	case o.Type == "event_msg":
		l.opener = o.Payload.Type == "task_started" || o.Payload.Type == "turn_started" || o.Payload.Type == "user_message"
	}
	return l
}

// Cut is where a rewind to the person's message entryID cuts the transcript: the start of the message's line, moved
// back over the lines that only open its turn. Claude's entry ids name the line's uuid ("c:<uuid>#k"), Codex's its
// offset ("x:<offset>#k").
func Cut(provider, path, entryID string) (int64, error) {
	id, _, ok := strings.Cut(entryID, "#")
	if !ok {
		return 0, ErrNotFound
	}
	read := claudeLine
	var uuid string
	offset := int64(-1)
	switch provider {
	case Claude:
		if uuid, ok = strings.CutPrefix(id, "c:"); !ok || uuid == "" {
			return 0, ErrNotFound
		}
	case Codex:
		read = codexLine
		s, isOffset := strings.CutPrefix(id, "x:")
		n, err := strconv.ParseInt(s, 10, 64)
		if !isOffset || err != nil || n < 0 {
			return 0, ErrNotFound
		}
		offset = n
	default:
		return 0, fmt.Errorf("fork: unknown provider %q", provider)
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var lines []line
	for at := int64(0); ; {
		raw, err := readLine(r)
		if len(raw) == 0 {
			if err == nil || errors.Is(err, io.EOF) {
				return 0, ErrNotFound
			}
			return 0, err
		}
		l := read(bytes.TrimSpace(raw))
		l.at = at
		if at == offset || uuid != "" && l.uuid == uuid {
			if !l.opener && !(provider == Claude && l.kind == "user") {
				return 0, ErrNotFound
			}
			return cutBefore(lines, at)
		}
		if offset >= 0 && at > offset {
			return 0, ErrNotFound
		}
		lines = append(lines, l)
		at += int64(len(raw))
		if err != nil {
			return 0, ErrNotFound
		}
	}
}

// cutBefore is the cut before the line at at, moved back over the lines before it that only open its turn; nothing
// kept when no message comes before it.
func cutBefore(lines []line, at int64) (int64, error) {
	cut := at
	i := len(lines) - 1
	for ; i >= 0 && lines[i].opener; i-- {
		cut = lines[i].at
	}
	for ; i >= 0; i-- {
		if lines[i].message {
			return cut, nil
		}
	}
	return 0, ErrNothingKept
}

// readLine is the next line with its newline; a line longer than the reader's buffer is read whole.
func readLine(r *bufio.Reader) ([]byte, error) {
	raw, err := r.ReadSlice('\n')
	for errors.Is(err, bufio.ErrBufferFull) {
		var more []byte
		more, err = r.ReadSlice('\n')
		raw = append(append([]byte{}, raw...), more...)
		if len(raw) > maxLine {
			return nil, errors.New("fork: a transcript line is too long")
		}
	}
	return raw, err
}

// Session is a forked session: its id and transcript.
type Session struct {
	ID   string
	Path string
}

// Write writes the first cut bytes of the transcript at path, whose session is oldID, as a new session of the agent
// and returns it. The old transcript is not touched.
func Write(provider, path, oldID string, cut int64, now time.Time) (Session, error) {
	if len(oldID) != 36 {
		return Session{}, fmt.Errorf("fork: %q is not a session id", oldID)
	}
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	head := make([]byte, cut)
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil {
		return Session{}, err
	}
	var s Session
	switch provider {
	case Claude:
		s.ID = uuid4()
		s.Path = filepath.Join(filepath.Dir(path), s.ID+".jsonl")
	case Codex:
		s.ID = uuid7(now)
		s.Path = filepath.Join(codexDay(path, now), "rollout-"+now.Format("2006-01-02T15-04-05")+"-"+s.ID+".jsonl")
	default:
		return Session{}, fmt.Errorf("fork: unknown provider %q", provider)
	}
	head = bytes.ReplaceAll(head, []byte(oldID), []byte(s.ID))
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return Session{}, err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, head, 0o600); err != nil {
		return Session{}, err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		os.Remove(tmp)
		return Session{}, err
	}
	return s, nil
}

// SessionID is the session of a transcript, read off its file name (Claude: <id>.jsonl, Codex: rollout-…-<id>.jsonl).
func SessionID(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if len(name) < 36 {
		return ""
	}
	return name[len(name)-36:]
}

// codexDay is today's folder of the sessions root a rollout lives under (…/sessions/YYYY/MM/DD), or the rollout's own
// folder when it is not laid out so.
func codexDay(path string, now time.Time) string {
	day := filepath.Dir(path)
	root := filepath.Dir(filepath.Dir(filepath.Dir(day)))
	if filepath.Base(root) != "sessions" {
		return day
	}
	return filepath.Join(root, now.Format("2006"), now.Format("01"), now.Format("02"))
}

func uuid4() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return format(b)
}

// uuid7 is a time-ordered uuid, the kind Codex names its threads with.
func uuid7(now time.Time) string {
	var b [16]byte
	rand.Read(b[6:])
	ms := uint64(now.UnixMilli())
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	return format(b)
}

func format(b [16]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, x := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[x>>4], hex[x&0x0f])
	}
	return string(out)
}
