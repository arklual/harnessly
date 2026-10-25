package typist

import (
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	imageConfirmBase     = 8 * time.Second
	imageConfirmPerImage = 1500 * time.Millisecond
	imagePoll            = 80 * time.Millisecond
)

var pathSpecialChar = regexp.MustCompile("[ \t\\\\'\"(){}\\[\\]!#$&;`|*?<>~^%]")

// EscapePathForPrompt puts a backslash before the characters an agent's prompt would read specially in a pasted path.
func EscapePathForPrompt(path string) string { return pathSpecialChar.ReplaceAllString(path, `\$0`) }

// pasteImages pastes picture paths and waits until the box holds a reference for each (Claude turns every path of
// one paste into its own reference; Codex reads one path per paste). landed are the references of this message the box
// held before. While Claude still reads the paste («Pasting…» under the box — big pictures on a loaded machine take
// seconds) nothing is pressed and the wait goes on, up to imagePastingMax: keys pressed meanwhile take nothing, and the
// references land after them. A paste that ends without its references (Claude kept the paths as text) is left in
// Claude's box: the message goes with the paths as text (typeRest), and the agent reads the files by them.
func pasteImages(paths []string, batch bool, landed int, p Pane, c Clock, provider wire.Provider) error {
	groups := [][]string{paths}
	if !batch {
		groups = nil
		for _, path := range paths {
			groups = append(groups, []string{path})
		}
	}
	before, _ := p.Capture()
	attached := 0
	all := make([]string, 0, len(paths))
	for _, g := range groups {
		escaped := make([]string, len(g))
		for i, path := range g {
			escaped[i] = EscapePathForPrompt(path)
		}
		all = append(all, escaped...)
		if err := p.Paste(strings.Join(escaped, " ")); err != nil {
			return err
		}
		attached += len(g)
		timeout := imageConfirmBase + imageConfirmPerImage*time.Duration(len(g)-1)
		if provider == wire.ProviderCodex {
			if !waitForImages(p, attached, before, timeout, c) {
				return errPicture
			}
			continue
		}
		if !waitForBoxImages(p, landed+attached, timeout, c) {
			return errPicture
		}
	}
	return nil
}

// imagePastingMax is the longest a paste of pictures is waited for while Claude says it still reads it.
const imagePastingMax = 60 * time.Second

// waitForBoxImages polls Claude's box until it holds expected references and Claude no longer reads the paste; past
// timeout only a paste still being read is waited for, up to imagePastingMax.
func waitForBoxImages(p Pane, expected int, timeout time.Duration, c Clock) bool {
	start := c.Now()
	for {
		view := Look(p, wire.ProviderClaude)
		reading := view.Readable && Pasting(view.Plain)
		if view.Readable && !reading && CountImageRefs(view.Box) >= expected {
			return true
		}
		waited := c.Now().Sub(start)
		if waited+imagePoll >= imagePastingMax || waited+imagePoll >= timeout && !reading {
			return false
		}
		c.Sleep(imagePoll)
	}
}

// waitForImages polls the plain screen until expected references are new on it, or timeout passes.
func waitForImages(p Pane, expected int, before string, timeout time.Duration, c Clock) bool {
	deadline := c.Now().Add(timeout)
	for {
		if s, ok := p.Capture(); ok && NewImageRefs(before, s) >= expected {
			return true
		}
		if !c.Now().Add(imagePoll).Before(deadline) {
			return false
		}
		c.Sleep(imagePoll)
	}
}
