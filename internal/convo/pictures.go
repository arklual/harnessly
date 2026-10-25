package convo

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The pictures a conversation's messages name by their path (the agent's «Скриншот: /tmp/…/home.png», the path of a
// picture pasted into the old view): found in the feed's texts on the actor, checked off it — the file is there, a
// picture, inside the folders the conversation may show — and listed in the snapshot by an id. Where the files are
// stays here: the page asks for one by its id, and the file is checked again when it is served.

const (
	// picturesEvery is how often the texts are looked through: a text already read is not read again.
	picturesEvery = 3 * time.Second
	// picturesRecheck is how often the same paths are checked again, for a file that came or went.
	picturesRecheck = 30 * time.Second
)

type pictureState struct {
	list    []wire.Picture
	paths   map[string]string // id → the path as the message writes it
	named   map[string]namedIn
	checked []string // the paths of the last check, in order
	at      time.Time
	reading bool
}

// namedIn is what one entry's text named, at its rev.
type namedIn struct {
	rev   int64
	paths []string
}

// pictureRoots are the folders the conversation may show a picture from: its own and the deployment's.
func (c *conv) pictureRoots() []string {
	if c.d.PictureRoots == nil {
		return nil
	}
	return append([]string{c.rec.Cwd}, c.d.PictureRoots...)
}

// namedPictures are the paths the messages name, each once by its latest mention, the latest last, at most
// wire.PicturesMax.
func (c *conv) namedPictures() []string {
	p := &c.pics
	seen := make(map[string]bool, len(p.named))
	var out []string
	c.fd.Messages(func(e wire.Entry) {
		seen[e.ID] = true
		n, ok := p.named[e.ID]
		if !ok || n.rev != e.Rev {
			text := e.MD
			if e.T == wire.EntryUser {
				text = e.Text
			}
			n = namedIn{rev: e.Rev, paths: shots.Named(text)}
			if p.named == nil {
				p.named = map[string]namedIn{}
			}
			p.named[e.ID] = n
		}
		for _, path := range n.paths {
			out = slices.DeleteFunc(out, func(x string) bool { return x == path })
			out = append(out, path)
		}
	})
	for id := range p.named {
		if !seen[id] {
			delete(p.named, id)
		}
	}
	if len(out) > wire.PicturesMax {
		out = out[len(out)-wire.PicturesMax:]
	}
	return out
}

// watchPictures looks through the texts now and then, and checks the paths they name off the actor when they changed
// or a while has passed.
func (c *conv) watchPictures(now time.Time) {
	p := &c.pics
	if c.d.PictureRoots == nil || c.deletedAt != 0 || c.fd == nil || p.reading || now.Sub(p.at) < picturesEvery {
		return
	}
	named := c.namedPictures()
	if slices.Equal(named, p.checked) && now.Sub(p.at) < picturesRecheck {
		return
	}
	if len(named) == 0 && len(p.list) == 0 {
		p.checked, p.at = named, now
		return
	}
	p.reading, p.at = true, now
	roots := c.pictureRoots()
	c.spawn(func() any { return checkPictures(named, roots) }, func(r any) {
		p.reading = false
		p.checked = named
		got, _ := r.([]checkedPicture)
		c.picturesApply(got)
	})
}

type checkedPicture struct {
	id, path string
}

// checkPictures keeps the paths that may be shown, each with an id of its path and its file's time: a picture written
// over is a new one to a page that kept the old bytes.
func checkPictures(paths, roots []string) []checkedPicture {
	var out []checkedPicture
	for _, path := range paths {
		if _, st, ok := shots.Allowed(filepath.Clean(path), roots); ok {
			out = append(out, checkedPicture{id: shots.ID(path, st.ModTime().UnixMilli()), path: path})
		}
	}
	return out
}

func (c *conv) picturesApply(got []checkedPicture) {
	if c.deletedAt != 0 {
		return
	}
	next := make([]wire.Picture, len(got))
	paths := make(map[string]string, len(got))
	for i, g := range got {
		next[i] = wire.Picture{ID: g.id, Text: g.path}
		paths[g.id] = filepath.Clean(g.path)
	}
	c.pics.paths = paths
	if slices.Equal(next, c.pics.list) {
		return
	}
	c.pics.list = next
	c.emit(wire.PicturesSetEv{Pictures: slices.Clone(next)})
}
