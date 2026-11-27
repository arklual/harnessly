package connect

import "sync"

// maxStream is how much of one stream a device keeps for a runtime that has not got it yet. A command that writes
// more while the link is down loses the start of what it wrote; the runtime is told how much (a gap).
const maxStream = 32 << 20

// obuf is one output stream of a command: everything from offset base on, the offset of a byte never changing.
type obuf struct {
	mu   sync.Mutex
	base int64
	data []byte
}

func (b *obuf) write(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if over := len(b.data) - maxStream; over > 0 {
		b.data = append(b.data[:0:0], b.data[over:]...)
		b.base += int64(over)
	}
}

// end is the offset after the last byte written.
func (b *obuf) end() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.base + int64(len(b.data))
}

// read gives at most max bytes from offset off on, and the offset they really start at (later than off when the
// bytes before were dropped).
func (b *obuf) read(off int64, max int) (int64, []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if off < b.base {
		off = b.base
	}
	i := int(off - b.base)
	if i >= len(b.data) {
		return off, nil
	}
	j := min(len(b.data), i+max)
	return off, append([]byte(nil), b.data[i:j]...)
}
