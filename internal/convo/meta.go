package convo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/sessionmeta"
	"github.com/ly-solutions/harnessly/internal/shots"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// metaEvery is how often a transcript is asked what it says about the session: the read is a stat, and the lines the
// transcript gained when it grew.
const metaEvery = 3 * time.Second

// metaRestEvery: a conversation at rest whose files did not change is still read whole this often, so what the record
// changed meanwhile meets the transcript's word again.
const metaRestEvery = time.Minute

// watchMeta reads, off the actor and now and then, the effort the agent really runs on its /goal and its recap, and gives them
// back on the actor. The transcript is the only place that says them: the box shows them, the hooks do not.
func (c *conv) watchMeta(now time.Time) {
	if c.d.Meta == nil && c.d.Shots == nil || c.deletedAt != 0 || c.rec.Provider != wire.ProviderClaude || c.rec.TranscriptPath == "" {
		return
	}
	if c.metaReading || now.Sub(c.metaAt) < metaEvery {
		return
	}
	c.metaReading, c.metaAt = true, now
	meta, pics, path := c.d.Meta, c.d.Shots, c.rec.TranscriptPath
	// A conversation at rest whose transcript and subagents folder did not change has nothing new to say. Every
	// conversation of the runtime — stopped and gone ones too, 260 on one machine — came here every 3 s, and the reads
	// (the transcript, the subagents folder, each subagent's file) were ~80 opens a second for nothing. A
	// working one is read whatever the stamp: a subagent's file grows without touching the folder.
	resting, last := c.metaResting() && now.Sub(c.metaFullAt) < metaRestEvery, c.metaStamp
	c.spawn(func() any {
		out := metaRead{stamp: stampOf(path)}
		if resting && out.stamp.ok && out.stamp == last {
			out.same = true
			return out
		}
		if meta != nil {
			out.info = meta.Read(path)
		}
		if pics != nil {
			out.shots = servable(pics.Read(path))
		}
		return out
	}, func(r any) {
		c.metaReading = false
		got, _ := r.(metaRead)
		c.metaStamp = got.stamp
		if got.same {
			return
		}
		c.metaFullAt = now
		now := c.d.Clock.Now()
		if c.d.Meta != nil {
			c.metaApply(got.info, now)
		}
		c.shotsApply(got.shots)
	})
}

// servable keeps the pictures whose file can still be handed over: a gone or unreadable one would only make the
// page ask for it and fail.
func servable(list []shots.Shot) []shots.Shot {
	out := make([]shots.Shot, 0, len(list))
	for _, s := range list {
		f, err := shots.Open(s.Path)
		if err != nil {
			continue
		}
		f.Body.Close()
		out = append(out, s)
	}
	return out
}

// metaRead is what one look at the transcripts found; same: nothing changed since the last, nothing was read.
type metaRead struct {
	info  sessionmeta.Info
	shots []shots.Shot
	stamp metaStamp
	same  bool
}

// metaStamp is a transcript and its subagents folder as a stat sees them; ok is false when the transcript is not there.
type metaStamp struct {
	ok            bool
	size, modNano int64
	subsNano      int64
}

func stampOf(transcript string) metaStamp {
	st, err := os.Stat(transcript)
	if err != nil {
		return metaStamp{}
	}
	s := metaStamp{ok: true, size: st.Size(), modNano: st.ModTime().UnixNano()}
	if sub, err := os.Stat(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")); err == nil {
		s.subsNano = sub.ModTime().UnixNano()
	}
	return s
}

// metaResting: the agent is not at work, so its transcript is all there is to watch.
func (c *conv) metaResting() bool {
	switch c.rec.State.K {
	case wire.StateIdle, wire.StateReady, wire.StateStopped, wire.StateGone, wire.StateLaunchFailed:
		return true
	}
	return false
}

// shotsApply makes the list of pictures the transcripts' latest: a change goes out whole. Where the files are stays
// here, off the wire: a picture is asked for by its id and found in this list or not at all.
func (c *conv) shotsApply(list []shots.Shot) {
	if c.deletedAt != 0 {
		return
	}
	next := make([]wire.Shot, len(list))
	paths := make(map[string]string, len(list))
	for i, s := range list {
		w := wire.Shot{ID: s.ID, Name: s.Name(), At: wire.Ms(s.At)}
		if s.Agent != "" {
			agent := s.Agent
			w.Agent = &agent
		}
		next[i] = w
		paths[s.ID] = s.Path
	}
	c.shotPaths = paths
	if len(next) == len(c.shots) && slices.EqualFunc(next, c.shots, sameShot) {
		return
	}
	c.shots = next
	c.emit(wire.ShotsSetEv{Shots: slices.Clone(next)})
}

func sameShot(a, b wire.Shot) bool {
	return a.ID == b.ID && a.Name == b.Name && a.At == b.At && (a.Agent == nil) == (b.Agent == nil) && (a.Agent == nil || *a.Agent == *b.Agent)
}

// metaApply makes the record say what the transcript says. An effort the transcript does not state stays as it was
// (the person's own pick, a launch's), and none is made up for a conversation that never had one.
func (c *conv) metaApply(info sessionmeta.Info, now time.Time) {
	if c.deletedAt != 0 {
		return
	}
	changed := false
	if m := c.rec.Model; m != nil {
		next := *m
		if e := wire.Effort(info.Effort); slices.Contains(wire.Efforts, e) {
			next.Effort = &e
		}
		// Ultracode is what the running agent last said of it (its commands' answers and Claude Code's own reminders
		// of the mode, over the whole transcript, not only the tail read). Only while the process has said nothing
		// does the launch stand (a launch with the `ultracode` setting, a Legacy tab launched with --effort ultracode):
		// the ask is not the observation. A change that waits is the runtime's to make: the transcript of before is not
		// believed over it (a resumed agent starts without the mode, while the old «Ultracode on» is still in its
		// transcript).
		if c.rec.PendingModel == nil {
			switch {
			case info.Ultracode == sessionmeta.On:
				next.Ultracode = true
			case info.Ultracode == sessionmeta.Off:
				next.Ultracode = false
			case c.launchUltracode:
				next.Ultracode = true
			}
		}
		if next.Ultracode != m.Ultracode || !sameEffort(next.Effort, m.Effort) {
			c.rec.Model = &next
			changed = true
		}
	}
	goal := goalOf(info.Goal)
	if (goal == nil) != (c.rec.Goal == nil) || goal != nil && *goal != *c.rec.Goal {
		c.rec.Goal = goal
		changed = true
	}
	recap := recapOf(info.Recap)
	if !c.recapStands(recap) {
		recap = nil // the person wrote after it: it is the recap of an earlier absence, not of what happens now
	}
	if (recap == nil) != (c.rec.Recap == nil) || recap != nil && *recap != *c.rec.Recap {
		c.rec.Recap = recap
		changed = true
	}
	if c.setAgentTitle(info.Title) {
		changed = true
	}
	if changed {
		c.touch()
		c.flush(now, false)
	}
}

// setAgentTitle names the conversation by the agent's own title. A title the person typed (a rename) stays whatever the
// agent says; one taken from the first message, or an earlier agent title, gives way. Reports whether the record changed.
func (c *conv) setAgentTitle(text string) bool {
	title := titleOf(strings.TrimSpace(text))
	if title == "" || c.rec.TitleSource == wire.TitleFromUser || c.rec.Title == title && c.rec.TitleSource == wire.TitleFromAgent {
		return false
	}
	c.rec.Title, c.rec.TitleSource = title, wire.TitleFromAgent
	return true
}

func sameEffort(a, b *wire.Effort) bool { return a == b || a != nil && b != nil && *a == *b }

func goalOf(g *sessionmeta.Goal) *wire.Goal {
	if g == nil {
		return nil
	}
	return &wire.Goal{Text: g.Text, Status: wire.GoalStatus(g.Status), Since: wire.Ms(g.Since)}
}

func recapOf(r *sessionmeta.Recap) *wire.Recap {
	if r == nil {
		return nil
	}
	return &wire.Recap{Text: r.Text, At: wire.Ms(r.At)}
}
