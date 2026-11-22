package transcript

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// ParseAntigravityContent normalizes native Antigravity JSONL records.
func ParseAntigravityContent(content string) []*jsvalue.Object {
	return ParseAntigravityContentAt(content, 0)
}

// ParseAntigravityContentAt names entries by their physical line offset, consistently across pages.
// Only newline-terminated records are consumed: a writer may still be extending the final line.
func ParseAntigravityContentAt(content string, start int64) []*jsvalue.Object {
	out := []*jsvalue.Object{}
	for {
		line, rest, ok := strings.Cut(content, "\n")
		if !ok {
			break
		}
		var r struct {
			Source   string `json:"source"`
			Type     string `json:"type"`
			Content  string `json:"content"`
			Thinking string `json:"thinking"`
			Created  string `json:"created_at"`
			Model    string `json:"model"`
			Input    int64  `json:"input_tokens"`
			Output   int64  `json:"output_tokens"`
			Cache    int64  `json:"cache_read_tokens"`
			Calls    []struct {
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
			} `json:"tool_calls"`
			Media []struct {
				MIME string `json:"mime_type"`
				URI  string `json:"uri"`
			} `json:"media"`
		}
		if json.Unmarshal([]byte(line), &r) == nil {
			at := float64(0)
			if t, err := time.Parse(time.RFC3339Nano, r.Created); err == nil {
				at = float64(t.UnixMilli())
			}
			k := 0
			add := func(typ string, pairs ...any) *jsvalue.Object {
				e := codexEntry(typ, at, pairs...)
				e.Set("id", wire.OffsetEntryID(start, k))
				k++
				out = append(out, e)
				return e
			}
			switch {
			case r.Source == "USER_EXPLICIT" && r.Type == "USER_INPUT":
				text := r.Content
				if _, after, found := strings.Cut(text, "<USER_REQUEST>"); found {
					real, _, closed := strings.Cut(after, "</USER_REQUEST>")
					if closed {
						text = real
					} else {
						text = ""
					}
				} else {
					text, _, _ = strings.Cut(text, "<ADDITIONAL_METADATA>")
					text, _, _ = strings.Cut(text, "<USER_SETTINGS_CHANGE>")
				}
				if text = strings.TrimSpace(text); text != "" {
					add("user-message", "text", text)
				}
			case r.Source == "MODEL" && r.Type == "PLANNER_RESPONSE":
				if strings.TrimSpace(r.Thinking) != "" {
					add("thinking", "thinking", r.Thinking)
				}
				if r.Content != "" || r.Model != "" {
					add("assistant-message", "markdown", r.Content, "model", r.Model, "usage", jsvalue.NewObject("input_tokens", float64(r.Input), "output_tokens", float64(r.Output), "cache_read_input_tokens", float64(r.Cache)))
				}
				for _, c := range r.Calls {
					if c.Name != "" {
						add("tool-call", "toolName", c.Name, "summary", string(c.Args))
					}
				}
			case r.Source == "MODEL" && r.Type == "GENERIC":
				// Native results have no call id. Do not infer success or attach them to the last call.
				e := add("antigravity-result", "stdout", r.Content)
				paths := []any{}
				for _, m := range r.Media {
					if strings.HasPrefix(m.MIME, "image/") {
						if u, err := url.Parse(m.URI); err == nil && u.Scheme == "file" && u.Host == "" {
							paths = append(paths, u.Path)
						}
					}
				}
				if len(paths) > 0 {
					e.Set("imagePaths", paths)
				}
			}
		}
		start += int64(len(line) + 1)
		content = rest
	}
	return out
}

// ParseAntigravityAppended consumes whole lines without retaining partial JSON.
func ParseAntigravityAppended(buf []byte, offset int64) Incremental {
	whole, partial := wholeLines(string(buf))
	return Incremental{NewEntries: ParseAntigravityContentAt(whole, offset), NewOffset: offset + int64(len(buf)) - partial}
}

// ReadAntigravityTail reads backwards in bounded windows until max entries are available.
func ReadAntigravityTail(path string, max int, roots ...string) Chunk {
	return ReadAntigravityBefore(path, -1, max, roots...)
}

// ReadAntigravityBefore reads entries whose physical lines precede before (-1 means EOF).
func ReadAntigravityBefore(path string, before int64, max int, roots ...string) Chunk {
	empty := Chunk{Entries: []*jsvalue.Object{}}
	f, err := OpenAntigravity(path, roots...)
	if err != nil {
		return empty
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return empty
	}
	end := st.Size()
	if before >= 0 && before < end {
		end = before
	}
	if end < 0 {
		return empty
	}
	window := int64(256 << 10)
	for {
		from := end - window
		if from < 0 {
			from = 0
		}
		buf := make([]byte, end-from)
		n, err := f.ReadAt(buf, from)
		if err == io.EOF {
			err = nil
		}
		buf = buf[:n]
		start := from
		if from > 0 {
			if i := strings.IndexByte(string(buf), '\n'); i >= 0 {
				start += int64(i + 1)
				buf = buf[i+1:]
			} else {
				start = end
				buf = nil
			}
		}
		text := claudeDecode(buf)
		if err != nil {
			return empty
		}
		whole, partial := wholeLines(text)
		entries := ParseAntigravityContentAt(whole, start)
		if from > 0 && (max <= 0 || len(entries) < max) {
			window *= 2
			continue
		}
		cut := 0
		if max > 0 && len(entries) > max {
			cut = len(entries) - max
			// Never split a native line: Before must be able to recover all older entries.
			id := entries[cut].String("id")
			prefix, _, _ := strings.Cut(id, "#")
			for cut > 0 && strings.HasPrefix(entries[cut-1].String("id"), prefix+"#") {
				cut--
			}
		}
		entries = entries[cut:]
		first := start
		if len(entries) > 0 { // OffsetEntryID is decoded through the same helper used by feed readers.
			first = antigravityEntryOffset(entries[0].String("id"), start)
		}
		return Chunk{Entries: entries, StartByteOffset: first, FileSize: end - partial, HasMore: first > 0}
	}
}

func antigravityEntryOffset(id string, fallback int64) int64 {
	line, _, _ := strings.Cut(strings.TrimPrefix(id, "x:"), "#")
	n, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

// OpenAntigravity confines every lookup (including symlinks) to a trusted root.
// Without an explicit root, native sessions live under the configured user home.
// Explicit roots support callers with a configured brain directory and parser fixtures.
func OpenAntigravity(path string, roots ...string) (*os.File, error) {
	root := ""
	if len(roots) > 0 {
		root = roots[0]
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	}
	if root == "" {
		return nil, fmt.Errorf("empty Antigravity root")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("Antigravity path outside root")
	}
	dir, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := dir.Open(rel)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("Antigravity transcript is not a regular file")
	}
	return f, nil
}
