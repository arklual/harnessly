package prompts

import (
	"regexp"
	"strings"
	"time"
)

const (
	imageConfirmBase     = 5 * time.Second
	imageConfirmPerImage = 1500 * time.Millisecond
	imagePoll            = 80 * time.Millisecond
	imageSettleBase      = 400 * time.Millisecond
	imageSettlePerImage  = 100 * time.Millisecond
)

var (
	imageRef        = regexp.MustCompile(`\[Image #\d+\]`)
	pathSpecialChar = regexp.MustCompile("[ \t\\\\'\"(){}\\[\\]!#$&;`|*?<>~^%]")
)

// CountImageRefs counts the `[Image #n]` references on a screen.
func CountImageRefs(screen string) int { return len(imageRef.FindAllStringIndex(screen, -1)) }

// EscapePathForPrompt puts a backslash before the characters an agent's prompt would read specially in a pasted path.
func EscapePathForPrompt(path string) string { return pathSpecialChar.ReplaceAllString(path, `\$0`) }

// ImageWait is how a wait for picture references ended.
type ImageWait struct {
	Confirmed  bool
	Attempts   int
	FinalCount int
	Elapsed    time.Duration
}

// WaitForImages polls the screen every poll until baseline+expected references show, or timeout passes. A screen that
// cannot be read is retried.
func WaitForImages(capture func() (string, bool), expected, baseline int, timeout, poll time.Duration, clock Clock) ImageWait {
	start := clock.Now()
	if expected <= 0 {
		return ImageWait{Confirmed: true, FinalCount: baseline}
	}
	target, deadline := baseline+expected, start.Add(timeout)
	w := ImageWait{FinalCount: baseline}
	for clock.Now().Before(deadline) {
		w.Attempts++
		if s, ok := capture(); ok {
			w.FinalCount = CountImageRefs(s)
			if w.FinalCount >= target {
				w.Confirmed = true
				break
			}
		}
		if !clock.Now().Add(poll).Before(deadline) {
			break
		}
		clock.Sleep(poll)
	}
	w.Elapsed = clock.Now().Sub(start)
	return w
}

// ImageSend is how pasting pictures ended.
type ImageSend struct {
	Confirmed          bool
	Attached, Expected int
	Elapsed            time.Duration
}

// SendImagePaths pastes picture paths into an agent's prompt and waits until the screen shows a reference for each.
// Claude Code turns every path of one paste into its own reference (batch); Codex reads one path per paste. paste gets
// the escaped, space-joined paths of one paste. Without capture nothing can be confirmed and each paste only gets a
// moment to settle.
func SendImagePaths(paths []string, batch bool, paste func(body string) error, capture func() (string, bool), clock Clock) ImageSend {
	start := clock.Now()
	res := ImageSend{Expected: len(paths)}
	if len(paths) == 0 {
		res.Confirmed = true
		return res
	}
	var groups [][]string
	if batch {
		groups = [][]string{paths}
	} else {
		for _, p := range paths {
			groups = append(groups, []string{p})
		}
	}
	body := func(group []string) string {
		escaped := make([]string, len(group))
		for i, p := range group {
			escaped[i] = EscapePathForPrompt(p)
		}
		return strings.Join(escaped, " ")
	}
	if capture == nil {
		for _, g := range groups {
			if paste(body(g)) != nil {
				res.Elapsed = clock.Now().Sub(start)
				return res
			}
			clock.Sleep(imageSettleBase + imageSettlePerImage*time.Duration(len(g)))
		}
		res.Confirmed, res.Attached, res.Elapsed = true, len(paths), clock.Now().Sub(start)
		return res
	}
	baseline := 0
	if s, ok := capture(); ok {
		baseline = CountImageRefs(s)
	}
	seen := baseline
	for _, g := range groups {
		if paste(body(g)) != nil {
			res.Attached, res.Elapsed = max(0, seen-baseline), clock.Now().Sub(start)
			return res
		}
		w := WaitForImages(capture, len(g), seen, imageConfirmBase+imageConfirmPerImage*time.Duration(len(g)-1), imagePoll, clock)
		seen = w.FinalCount
		if !w.Confirmed {
			res.Attached, res.Elapsed = max(0, seen-baseline), clock.Now().Sub(start)
			return res
		}
	}
	res.Confirmed, res.Attached, res.Elapsed = true, len(paths), clock.Now().Sub(start)
	return res
}
