package wire

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// OpenTab is the tab of the right pane a link or a name of the chat opens in: a tracker task, a GitLab merge request or
// commit, a published document or board, or any other site in the browser.
type OpenTab string

const (
	OpenTask      OpenTab = "task"
	OpenGitLab    OpenTab = "gitlab"
	OpenArtifacts OpenTab = "artifacts"
	OpenBrowser   OpenTab = "browser"
)

// OpenTabs is every OpenTab, in the schema's order.
var OpenTabs = []OpenTab{OpenTask, OpenGitLab, OpenArtifacts, OpenBrowser}

// OpenRefMax is the longest ref the page may ask about, in bytes.
const OpenRefMax = 512

// ErrNothingToOpen: the ref is neither a task, a merge request, a document nor a site; the page opens nothing.
var ErrNothingToOpen = errors.New("nothing to open")

// OpenRefsMax is the most refs one GET .../open-refs may ask about; OpenChoicesMax the most candidates one ref gets.
const (
	OpenRefsMax    = 32
	OpenChoicesMax = 8
)

// OpenTarget is the answer of GET /api/v2/conversations/{id}/open?ref=<ref>: the tab and what it shows. A ref that
// fits several places (`!12` is a merge request of two projects the chat touches) lists them all in Choices, the best
// first, and Tab and Target are the first one's; a client that cannot ask the person opens that.
type OpenTarget struct {
	Tab     OpenTab      `json:"tab"`
	Target  string       `json:"target"`
	Choices []OpenChoice `json:"choices,omitempty"`
}

// OpenChoice is one place a ref may open: what the person picks from, Label the project and Detail what is known of
// the merge request (its title).
type OpenChoice struct {
	Tab    OpenTab `json:"tab"`
	Target string  `json:"target"`
	Label  string  `json:"label"`
	Detail *string `json:"detail"`
}

func (c OpenChoice) Validate() error {
	return errors.Join(in("tab", c.Tab, OpenTabs), need("target", c.Target), need("label", c.Label), needPtr("detail", c.Detail))
}

func (o OpenTarget) Validate() error {
	err := errors.Join(in("tab", o.Tab, OpenTabs), need("target", o.Target))
	if len(o.Choices) == 1 || len(o.Choices) > OpenChoicesMax {
		err = errors.Join(err, fmt.Errorf("choices: %d, want 2 to %d or none", len(o.Choices), OpenChoicesMax))
	}
	for _, c := range o.Choices {
		err = errors.Join(err, c.Validate())
	}
	if len(o.Choices) > 1 && (o.Choices[0].Tab != o.Tab || o.Choices[0].Target != o.Target) {
		err = errors.Join(err, errors.New("choices: the first is not tab and target"))
	}
	return err
}

// OpenedRef is where one ref of a batch opens; Open is null for what opens nothing.
type OpenedRef struct {
	Ref  string      `json:"ref"`
	Open *OpenTarget `json:"open"`
}

// OpenedRefs is the answer of GET /api/v2/conversations/{id}/open-refs?ref=<ref>&ref=<ref>…: every ref asked, in the
// order asked, with where it opens. It is what the page asks for the merge requests of a message before it draws
// them as something to click.
type OpenedRefs struct {
	Refs []OpenedRef `json:"refs"`
}

func (o OpenedRefs) Validate() error {
	var err error
	for _, r := range o.Refs {
		err = errors.Join(err, need("ref", r.Ref))
		if r.Open != nil {
			err = errors.Join(err, r.Open.Validate())
		}
	}
	return err
}

// open serves GET /api/v2/conversations/{id}/open: where a chip or a link of the conversation opens. It reads and
// changes nothing, so it needs no guard against other origins.
func (s *Server) open(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	if s.Open == nil {
		refuse(w, RefuseNotFound)
		return
	}
	ref := r.URL.Query().Get("ref")
	if ref == "" || len(ref) > OpenRefMax || !utf8.ValidString(ref) {
		refuse(w, RefuseBadRequest)
		return
	}
	target, err := s.Open(ctx, conv, ref)
	switch {
	case errors.Is(err, ErrNothingToOpen), err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil:
		agent.Log("warn", "v2: open failed", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		s.writeChecked(w, r, target)
	}
}

// openRefs serves GET /api/v2/conversations/{id}/open-refs: open for each ref of the query, none of them an error.
func (s *Server) openRefs(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	if s.Open == nil && s.OpenAll == nil {
		refuse(w, RefuseNotFound)
		return
	}
	refs := r.URL.Query()["ref"]
	if len(refs) == 0 || len(refs) > OpenRefsMax {
		refuse(w, RefuseBadRequest)
		return
	}
	for _, ref := range refs {
		if ref == "" || len(ref) > OpenRefMax || !utf8.ValidString(ref) {
			refuse(w, RefuseBadRequest)
			return
		}
	}
	var targets []*OpenTarget
	var err error
	if s.OpenAll != nil {
		targets, err = s.OpenAll(ctx, conv, refs)
		if err == nil && len(targets) != len(refs) {
			err = fmt.Errorf("open: %d answers for %d refs", len(targets), len(refs))
		}
	} else {
		targets = make([]*OpenTarget, len(refs))
		for i, ref := range refs {
			t, one := s.Open(ctx, conv, ref)
			switch {
			case errors.Is(one, ErrNothingToOpen):
			case one != nil:
				err = one
			default:
				targets[i] = &t
			}
			if err != nil {
				break
			}
		}
	}
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil:
		agent.Log("warn", "v2: open-refs failed", agent.F{K: "path", V: r.URL.Path}, agent.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		out := OpenedRefs{Refs: make([]OpenedRef, len(refs))}
		for i, ref := range refs {
			out.Refs[i] = OpenedRef{Ref: ref, Open: targets[i]}
		}
		s.writeChecked(w, r, out)
	}
}
