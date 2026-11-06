// Package secretmask replaces the values of secrets in the output of a command by a mark.
package secretmask

import (
	"bytes"
	"io"
	"sort"
	"sync"
)

// Masker writes through to w with every value replaced by a mark. It holds back only the end of what it was given that
// could be the start of a value, so a value split between two writes is masked too, and output is not delayed otherwise.
type Masker struct {
	w      io.Writer
	values [][]byte
	mu     sync.Mutex
	held   []byte
}

// Mark stands for a value in the output.
var Mark = []byte("••••••")

// MinMasked: shorter values are not masked (they would mangle ordinary output).
const MinMasked = 4

// New masks values in what is written to w.
func New(w io.Writer, values []string) *Masker {
	m := &Masker{w: w}
	for _, v := range values {
		if len(v) >= MinMasked {
			m.values = append(m.values, []byte(v))
		}
	}
	// The longer first: a value that contains another is masked whole.
	sort.Slice(m.values, func(i, j int) bool { return len(m.values[i]) > len(m.values[j]) })
	return m
}

func (m *Masker) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := append(m.held, p...)
	for _, v := range m.values {
		buf = bytes.ReplaceAll(buf, v, Mark)
	}
	keep := 0
	for _, v := range m.values {
		for n := min(len(v)-1, len(buf)); n > keep; n-- {
			if bytes.HasSuffix(buf, v[:n]) {
				keep = n
				break
			}
		}
	}
	m.held = append([]byte(nil), buf[len(buf)-keep:]...)
	if _, err := m.w.Write(buf[:len(buf)-keep]); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush writes what was held back: the output ended, it was not a value.
func (m *Masker) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.held) > 0 {
		_, _ = m.w.Write(m.held)
		m.held = nil
	}
}
