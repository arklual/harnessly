// Package usageoutbox is the machine's outbox of spend facts (docs/hub/usage.md): records with a monotonic seq within an
// epoch, kept on disk until the hub says it has stored them. The hub pulls them (usage.read) and acknowledges
// (usage.ack); nothing here knows what a record means, and none of them carries the text of a conversation.
//
// The folder holds two files. records.jsonl is one JSON record per line, in seq order, appended and synced; meta.json
// holds the epoch, the last acknowledged seq and whether the hub has ever read. A folder that is gone is a new epoch.
package usageoutbox

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	recordsFile = "records.jsonl"
	metaFile    = "meta.json"
	// compactBytes: the acknowledged head is cut off the file once it is this large and not smaller than the rest, so a
	// long-running machine does not grow the file without end.
	compactBytes = 1 << 20
)

type meta struct {
	Epoch string `json:"epoch"`
	Acked int64  `json:"acked"`
	// Asked: the hub has read this epoch at least once, so it is interested in what is written.
	Asked bool `json:"asked,omitempty"`
}

// Outbox is safe for concurrent use.
type Outbox struct {
	dir string
	now func() time.Time

	mu    sync.Mutex
	epoch string
	acked int64
	asked bool
	// first is the seq of the record at offs[0]; seqs are consecutive.
	first int64
	// offs are the offsets of the records in the file, so a read seeks and memory stays small for a long backfill.
	offs []int64
	size int64
	f    *os.File
}

// Open makes the outbox in dir, or opens the one that is there. A missing or unreadable meta file makes a new epoch
// and drops whatever records were there: they belong to an epoch the hub may no longer know. now is the clock (nil is
// the real one).
func Open(dir string, now func() time.Time) (*Outbox, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	o := &Outbox{dir: dir, now: now}
	raw, err := os.ReadFile(filepath.Join(dir, metaFile))
	var m meta
	if err != nil || json.Unmarshal(raw, &m) != nil || m.Epoch == "" || m.Acked < 0 {
		if err := o.fresh(); err != nil {
			return nil, err
		}
		return o, nil
	}
	o.epoch, o.acked, o.asked = m.Epoch, m.Acked, m.Asked
	if err := o.load(); err != nil {
		return nil, err
	}
	return o, nil
}

// fresh starts a new epoch with no records.
func (o *Outbox) fresh() error {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	o.epoch, o.acked, o.asked, o.first, o.offs, o.size = hex.EncodeToString(b[:]), 0, false, 1, nil, 0
	if err := os.WriteFile(filepath.Join(o.dir, recordsFile), nil, 0o600); err != nil {
		return err
	}
	if err := o.writeMeta(); err != nil {
		return err
	}
	return o.openFile()
}

func (o *Outbox) openFile() error {
	f, err := os.OpenFile(filepath.Join(o.dir, recordsFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	o.f = f
	return nil
}

// load indexes the records file. A torn last line (the machine stopped mid-write) is cut off.
func (o *Outbox) load() error {
	path := filepath.Join(o.dir, recordsFile)
	raw, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		o.first, o.offs, o.size = o.acked+1, nil, 0
		return o.openFile()
	}
	if err != nil {
		return err
	}
	r := bufio.NewReaderSize(raw, 64<<10)
	var off int64
	o.first, o.offs = o.acked+1, nil
	for {
		line, err := r.ReadBytes('\n')
		if err != nil || !bytes.HasSuffix(line, []byte("\n")) {
			break
		}
		if len(o.offs) == 0 {
			var head struct {
				Seq int64 `json:"seq"`
			}
			if json.Unmarshal(line, &head) != nil || head.Seq < 1 {
				break
			}
			o.first = head.Seq
		}
		o.offs = append(o.offs, off)
		off += int64(len(line))
	}
	raw.Close()
	o.size = off
	if err := os.Truncate(path, off); err != nil {
		return err
	}
	// Records the meta file already counts as acknowledged are not offered again.
	o.dropAcked()
	if len(o.offs) == 0 && o.first <= o.acked {
		o.first = o.acked + 1
	}
	return o.openFile()
}

// dropAcked forgets the head of the index that is acknowledged. The file is cut by compact.
func (o *Outbox) dropAcked() {
	for len(o.offs) > 0 && o.first <= o.acked {
		o.offs = o.offs[1:]
		o.first++
	}
}

func (o *Outbox) writeMeta() error {
	raw, err := json.Marshal(meta{Epoch: o.epoch, Acked: o.acked, Asked: o.asked})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(o.dir, metaFile), raw)
}

func writeAtomic(path string, raw []byte) error {
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Epoch is the id of the epoch this outbox is in.
func (o *Outbox) Epoch() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.epoch
}

// lastLocked is the seq of the newest record written, acknowledged or not.
func (o *Outbox) lastLocked() int64 {
	if len(o.offs) == 0 {
		return o.acked
	}
	return o.first + int64(len(o.offs)) - 1
}

// Last is the seq of the newest record written (0 when none ever was).
func (o *Outbox) Last() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastLocked()
}

// Pending is how many records wait for an acknowledgement.
func (o *Outbox) Pending() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.offs)
}

// PendingBytes is how many bytes of the records file wait for an acknowledgement.
func (o *Outbox) PendingBytes() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.offs) == 0 {
		return 0
	}
	return o.size - o.offs[0]
}

// Asked says whether the hub has read this epoch. It is kept on disk, so a restarted machine knows the hub is interested
// without waiting for a read.
func (o *Outbox) Asked() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.asked
}

// Append gives the records their seqs and the machine's time, writes them in one synced write and returns the seq of
// the last. The records passed in are not changed.
func (o *Outbox) Append(recs []hubproto.UsageRecord) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(recs) == 0 {
		return o.lastLocked(), nil
	}
	at := o.now().UnixMilli()
	seq := o.lastLocked()
	var buf bytes.Buffer
	offs := make([]int64, 0, len(recs))
	for _, r := range recs {
		seq++
		r.Seq, r.At = seq, at
		line, err := json.Marshal(r)
		if err != nil {
			return o.lastLocked(), err
		}
		offs = append(offs, o.size+int64(buf.Len()))
		buf.Write(line)
		buf.WriteByte('\n')
	}
	// A failed write may leave part of a line; the next start cuts it, and this process goes back to where it was.
	if _, err := o.f.WriteAt(buf.Bytes(), o.size); err != nil {
		_ = o.f.Truncate(o.size)
		return o.lastLocked(), err
	}
	if err := o.f.Sync(); err != nil {
		_ = o.f.Truncate(o.size)
		return o.lastLocked(), err
	}
	if len(o.offs) == 0 {
		o.first = o.acked + 1
	}
	o.offs = append(o.offs, offs...)
	o.size += int64(buf.Len())
	return seq, nil
}

// Read answers usage.read. The records are those with seq > after of the asked epoch, at most limit (clamped to
// 1..hubproto.UsageReadMax); a different epoch is read from its beginning. The same epoch and after give the same
// records until they are acknowledged.
func (o *Outbox) Read(epoch string, after int64, limit int) (hubproto.UsageReadResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	limit = max(1, min(limit, hubproto.UsageReadMax))
	if !o.asked {
		// Best effort: if the marker cannot be written the hub's next announcement brings the next read.
		o.asked = true
		_ = o.writeMeta()
	}
	if epoch != o.epoch || after < 0 {
		after = 0
	}
	res := hubproto.UsageReadResult{Epoch: o.epoch, Records: []hubproto.UsageRecord{}, Last: after}
	// What is acknowledged is gone: a cursor behind it reads from the first record still here.
	start := max(after+1, o.first)
	last := o.lastLocked()
	if len(o.offs) == 0 || start > last {
		return res, nil
	}
	n := int(min(int64(limit), last-start+1))
	from := o.offs[start-o.first]
	to := o.size
	if end := start - o.first + int64(n); end < int64(len(o.offs)) {
		to = o.offs[end]
	}
	buf := make([]byte, to-from)
	if _, err := o.f.ReadAt(buf, from); err != nil {
		return res, err
	}
	for _, line := range bytes.Split(bytes.TrimRight(buf, "\n"), []byte("\n")) {
		var rec hubproto.UsageRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return res, fmt.Errorf("usage outbox: record at seq %d: %w", start+int64(len(res.Records)), err)
		}
		res.Records = append(res.Records, rec)
	}
	res.Last = res.Records[len(res.Records)-1].Seq
	res.More = res.Last < last
	return res, nil
}

// Ack answers usage.ack: records up to seq of this epoch are dropped. Another epoch's ack, or one for a seq already
// acknowledged, changes nothing; a seq beyond the newest record counts as the newest.
func (o *Outbox) Ack(epoch string, seq int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if epoch != o.epoch || seq <= o.acked {
		return nil
	}
	seq = min(seq, o.lastLocked())
	if seq <= o.acked {
		return nil
	}
	o.acked = seq
	if err := o.writeMeta(); err != nil {
		return err
	}
	o.dropAcked()
	if len(o.offs) == 0 {
		o.first = o.acked + 1
	}
	return o.compactLocked()
}

// compactLocked rewrites the file without the acknowledged head once that head is large. The index already forgot the
// head; the file still has it, at the front.
func (o *Outbox) compactLocked() error {
	dead, live := o.size, int64(0)
	if len(o.offs) > 0 {
		dead, live = o.offs[0], o.size-o.offs[0]
	}
	if dead < compactBytes || dead < live {
		return nil
	}
	buf := make([]byte, live)
	if live > 0 {
		if _, err := o.f.ReadAt(buf, dead); err != nil {
			return err
		}
	}
	if err := writeAtomic(filepath.Join(o.dir, recordsFile), buf); err != nil {
		return err
	}
	o.f.Close()
	for i := range o.offs {
		o.offs[i] -= dead
	}
	o.size = live
	return o.openFile()
}

// Close closes the file.
func (o *Outbox) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.f == nil {
		return nil
	}
	err := o.f.Close()
	o.f = nil
	return err
}
