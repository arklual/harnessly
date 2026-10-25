package handoff

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// ErrNoCut: the transcript has no line of the turn a fork is to be cut before.
var ErrNoCut = errors.New("the transcript has no such turn")

// lineHead is what a Claude transcript line says about itself that a fork reads.
type lineHead struct {
	Type string `json:"type"`
	UUID string `json:"uuid"`
}

// UUIDOfTurn is the transcript line a Claude turn id names ("c:<uuid>#<k>"); "" for another id.
func UUIDOfTurn(turnID string) string {
	line, _, ok := strings.Cut(turnID, "#")
	uuid, isClaude := strings.CutPrefix(line, "c:")
	if !ok || !isClaude {
		return ""
	}
	return uuid
}

// ForkClaude writes a Claude session of its own for a fork: the lines of the transcript at src before the line whose
// uuid is cutBefore (the whole transcript when it is ""), each with its session id made newID, into a file newID.jsonl
// beside it, whose path it returns. The queue's own lines that stand right before the cut belong to the turn cut off
// and stay out of the copy. Claude resumes the copy as it resumes any session it kept: the fork starts with the history
// up to the cut, and the conversation it came from is not touched.
func ForkClaude(src, oldID, newID, cutBefore string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	dst := filepath.Join(filepath.Dir(src), newID+".jsonl")
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	w := bufio.NewWriter(out)
	from := []byte(`"sessionId":"` + oldID + `"`)
	to := []byte(`"sessionId":"` + newID + `"`)
	var held [][]byte
	found := cutBefore == ""
	r := bufio.NewReader(in)
	for {
		line, readErr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var head lineHead
			_ = json.Unmarshal(line, &head)
			if cutBefore != "" && head.UUID == cutBefore {
				found = true
				break
			}
			if !bytes.HasSuffix(line, []byte("\n")) {
				line = append(line, '\n')
			}
			line = bytes.ReplaceAll(line, from, to)
			if head.Type == "queue-operation" {
				held = append(held, line)
			} else {
				for _, h := range held {
					_, _ = w.Write(h)
				}
				held = nil
				_, _ = w.Write(line)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			return "", readErr
		}
	}
	if !found {
		out.Close()
		return "", ErrNoCut
	}
	if cutBefore == "" {
		for _, h := range held {
			_, _ = w.Write(h)
		}
	}
	if err := w.Flush(); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

// Cut is where a fork of turns cuts: after the turn at, or after the last finished one when at is "". next is the
// turn that opens right after the cut ("" when the cut is the transcript's end); ok is false when at is not a
// finished turn of them. working says the last turn still runs.
func Cut(turns []wire.Turn, at string, working bool) (keep wire.Turn, next string, ok bool) {
	last := len(turns) - 1
	if working || last >= 0 && turns[last].Status == wire.TurnLive {
		last--
	}
	if at == "" {
		if last < 0 {
			return wire.Turn{}, "", false
		}
		at = turns[last].ID
	}
	for i, t := range turns {
		if t.ID != at {
			continue
		}
		if i > last {
			return wire.Turn{}, "", false
		}
		if i+1 < len(turns) {
			next = turns[i+1].ID
		}
		return t, next, true
	}
	return wire.Turn{}, "", false
}
