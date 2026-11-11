package main

import (
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What the library's list reads from disk for every project and every conversation, kept between two lists: the page
// asks for the list every few seconds while an agent draws, and a library of many projects on a slow disk took longer
// to read than the page waited to ask again. A project's part is kept until the library's watcher sees its folder
// change; a conversation's links are kept until the conversation moves on. A list of an unchanged library then reads
// nothing of them, and while an agent draws only its project and its conversation are read again.

// designlyLinksFor bounds how long a quiet conversation's links are kept: the index of a long transcript is read
// behind (ChatLinks.Run), and what it finds there comes without the conversation moving.
const designlyLinksFor = time.Minute

// designlyProject is what the list reads of one project's folder besides its boards.
type designlyProject struct {
	video, deck bool
	owner       design.Owner
	owned       bool
}

type designlyProjectAt struct {
	designlyProject
	gen uint64
}

type designlyLinksAt struct {
	last    wire.Ms
	at      time.Time
	designs []string
}

type designlyDisk struct {
	mu sync.Mutex
	// root is the library folder the projects' parts were read from: a library that moved is read anew.
	root     string
	projects map[string]designlyProjectAt
	links    map[string]designlyLinksAt // by transcript
	// seeded is the library folder that was seen to have the demo projects copied.
	seeded string
}

func readDesignlyProject(lib *design.Library, id string) designlyProject {
	p := designlyProject{video: lib.IsVideo(id), deck: lib.IsDeck(id)}
	p.owner, p.owned = lib.ReadOwner(id)
	return p
}

// project is the project's part, read (through read) when the folder changed since it was kept or is not watched.
func (c *designlyDisk) project(lib *design.Library, id string, read func(*design.Library, string) designlyProject) designlyProject {
	gen, watched := lib.ListingGen(id)
	if !watched {
		return read(lib, id)
	}
	root := lib.Root()
	c.mu.Lock()
	if c.root != root {
		c.root, c.projects = root, nil
	}
	kept, ok := c.projects[id]
	c.mu.Unlock()
	if ok && kept.gen == gen {
		return kept.designlyProject
	}
	p := read(lib, id)
	c.mu.Lock()
	if c.root == root {
		if c.projects == nil {
			c.projects = map[string]designlyProjectAt{}
		}
		c.projects[id] = designlyProjectAt{designlyProject: p, gen: gen}
	}
	c.mu.Unlock()
	return p
}

// linked are the designs each transcript worked on (as ChatLinks.Designs answers), read through read only for the
// conversations that moved on since their links were kept; the conversations that are gone are forgotten.
func (c *designlyDisk) linked(convs []wire.Conversation, now time.Time, read func(paths []string) map[string][]string) map[string][]string {
	out := map[string][]string{}
	last := map[string]wire.Ms{}
	var stale []string
	c.mu.Lock()
	for _, conv := range convs {
		p := conv.TranscriptPath
		if p == "" {
			continue
		}
		last[p] = conv.LastActivityAt
		kept, ok := c.links[p]
		if ok && kept.last == conv.LastActivityAt && now.Sub(kept.at) < designlyLinksFor {
			if len(kept.designs) > 0 {
				out[p] = kept.designs
			}
			continue
		}
		stale = append(stale, p)
	}
	c.mu.Unlock()
	var fresh map[string][]string
	if len(stale) > 0 {
		fresh = read(stale)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.links == nil {
		c.links = map[string]designlyLinksAt{}
	}
	for _, p := range stale {
		c.links[p] = designlyLinksAt{last: last[p], at: now, designs: fresh[p]}
		if len(fresh[p]) > 0 {
			out[p] = fresh[p]
		}
	}
	for p := range c.links {
		if _, here := last[p]; !here {
			delete(c.links, p)
		}
	}
	return out
}

// demoSeeded: the demo projects were copied once; that is asked of the disk until it is so, and not after.
func (c *designlyDisk) demoSeeded(lib *design.Library) bool {
	root := lib.Root()
	c.mu.Lock()
	seeded := root != "" && c.seeded == root
	c.mu.Unlock()
	if seeded {
		return true
	}
	if !lib.DemoSeeded() {
		return false
	}
	c.mu.Lock()
	c.seeded = root
	c.mu.Unlock()
	return true
}
