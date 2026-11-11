package convo

import (
	"os"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/feed"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A conversation at rest writes down, with its record, what a start of the runtime read out of its transcript: the
// feed's place and summary (the last turn and its snippet, the context, the model, the tasks), what the runtime set
// on the feed, and the time of the person's last message. It is tied to the transcript as a stat sees it. A start
// that finds the same file takes it and reads nothing: the record already holds what the list and the card show, and
// the feed reads its bytes when a page first asks for the chat. A file that changed, or a chat that was not at rest,
// reads its transcript's tail as before.

// restFile is that part of a conversation's file.
type restFile struct {
	Stamp       fileStamp  `json:"stamp"`
	Feed        feed.Saved `json:"feed"`
	LastWroteAt wire.Ms    `json:"lastWroteAt,omitempty"`
}

// fileStamp is a file as a stat sees it: a transcript is only ever appended to, or replaced by another file.
type fileStamp struct {
	Size    int64  `json:"size"`
	ModNano int64  `json:"modNano"`
	Ino     uint64 `json:"ino,omitempty"`
}

func stampFile(path string) (fileStamp, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return fileStamp{}, false
	}
	s := fileStamp{Size: st.Size(), ModNano: st.ModTime().UnixNano()}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		s.Ino = uint64(sys.Ino)
	}
	return s, true
}

// rest is what the conversation writes down of its transcript; nil unless its feed is at rest and has read the file
// to its end.
func (c *conv) rest() *restFile {
	if c.fd == nil || c.rec.TranscriptPath == "" {
		return nil
	}
	s, ok := c.fd.Save()
	if !ok {
		return nil
	}
	st, ok := stampFile(c.rec.TranscriptPath)
	if !ok || st.Size != s.Size || !endsWithNewline(c.rec.TranscriptPath, st.Size) {
		return nil
	}
	return &restFile{Stamp: st, Feed: s, LastWroteAt: c.lastWroteAt}
}

// endsWithNewline says the file's last line is whole. A line still being written was read by the feed's poll, and a
// read of the same bytes later takes whole lines only: such a transcript is read at a start as before.
func endsWithNewline(path string, size int64) bool {
	if size == 0 {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [1]byte
	_, err = f.ReadAt(b[:], size-1)
	return err == nil && b[0] == '\n'
}

// wakeAtRest takes what a stopped conversation wrote down of its transcript when the file is still the one it was
// written over; false leaves the conversation to read its transcript. Called before the conversation has an actor.
func (c *conv) wakeAtRest(r *restFile) bool {
	if r == nil || c.rec.TranscriptPath == "" {
		return false
	}
	switch c.rec.State.K {
	case wire.StateStopped, wire.StateGone:
	default:
		// An agent that was running may have gone on while the runtime was away; its transcript is read.
		return false
	}
	if st, ok := stampFile(c.rec.TranscriptPath); !ok || st != r.Stamp {
		return false
	}
	if !c.fd.Restore(r.Feed) {
		return false
	}
	c.lastWroteAt = r.LastWroteAt
	c.unread = true
	return true
}

// asleep says the conversation came back at rest and nobody has asked for it since: its transcript says nothing the
// record does not hold, so the reads that follow a transcript on the clock leave it alone. It ends for good with the
// first snapshot, the first read of the feed, or an agent seen running.
func (c *conv) asleep() bool {
	if !c.unread {
		return false
	}
	switch c.rec.State.K {
	case wire.StateStopped, wire.StateGone, wire.StateLaunchFailed, wire.StateUnknown:
		if c.fd.Released() && !c.bgOpened {
			return true
		}
	}
	c.unread = false
	return false
}
