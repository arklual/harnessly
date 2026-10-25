package screen

import (
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// TrustPrompt is an agent asking whether to trust the folder it starts in.
type TrustPrompt struct {
	// Agent is "claude" or "codex".
	Agent      string `json:"agent"`
	FolderPath string `json:"folderPath"`
}

type trustSpec struct {
	agent      string
	signatures []*regexp.Regexp
	path       func(lines []string) string
}

var (
	absPathRE       = re(`(/[^` + spBody + `│]+)`)
	claudeAnchorRE  = re(`(?i)Quick safety check`)
	codexYouAreInRE = re(`(?i)You are in` + sp + `+(/` + nsp + `+)`)
	codexGitRootRE  = re(`(?i)repository root:` + sp + `*(/` + nsp + `+)`)
)

// pathIn is the trimmed first group of the first line where re finds a path longer than "/".
func pathIn(lines []string, pattern *regexp.Regexp) string {
	for _, line := range lines {
		if m := pattern.FindStringSubmatch(line); m != nil && jscompat.Len(m[1]) > 1 {
			return trim(m[1])
		}
	}
	return ""
}

// wrappedTail is the rest of a folder Claude wraps at the pane's width: the lines right under it, up to the blank one,
// each a single run of path characters (Claude 2.1.310 prints the folder bare, not in a box).
func wrappedTail(lines []string) string {
	var tail strings.Builder
	for _, line := range lines {
		t := trim(line)
		if !wrapRunRE.MatchString(t) {
			break
		}
		tail.WriteString(t)
	}
	return tail.String()
}

var wrapRunRE = re(`^[^` + spBody + `│/][^` + spBody + `│]*$`)

var trustSpecs = []trustSpec{
	{
		agent: "claude",
		signatures: []*regexp.Regexp{
			re(`(?i)Quick safety check`),
			re(`(?i)Yes,` + sp + `+I trust this folder`),
			re(`(?i)No,` + sp + `+exit`),
		},
		// The folder is printed at most 6 lines above the "Quick safety check" paragraph.
		path: func(lines []string) string {
			anchor := -1
			for i, line := range lines {
				if claudeAnchorRE.MatchString(line) {
					anchor = i
					break
				}
			}
			if anchor < 0 {
				return ""
			}
			for i := anchor - 1; i >= max(0, anchor-6); i-- {
				if m := absPathRE.FindStringSubmatch(lines[i]); m != nil && jscompat.Len(m[1]) > 1 {
					return trim(m[1]) + wrappedTail(lines[i+1:anchor])
				}
			}
			return ""
		},
	},
	{
		agent: "codex",
		signatures: []*regexp.Regexp{
			re(`(?i)Do you trust the contents of this directory`),
			re(`(?i)Yes,` + sp + `+continue`),
			re(`(?i)No,` + sp + `+quit`),
		},
		// Trust applies to the repository root when Codex names one, else to the folder it is in.
		path: func(lines []string) string {
			if p := pathIn(lines, codexGitRootRE); p != "" {
				return p
			}
			return pathIn(lines, codexYouAreInRE)
		},
	},
}

// MatchTrustPrompt reads Claude's or Codex's folder trust dialog and the folder it is about; nil when the text is not
// such a dialog or names no absolute path.
func MatchTrustPrompt(text string) *TrustPrompt {
	lines := strings.Split(text, "\n")
	for _, spec := range trustSpecs {
		matched := true
		for _, sig := range spec.signatures {
			if !sig.MatchString(text) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if path := spec.path(lines); path != "" {
			return &TrustPrompt{Agent: spec.agent, FolderPath: path}
		}
	}
	return nil
}

var (
	trustYesRE    = re(`(?i)Yes,` + sp + `+(?:I trust this folder|continue)`)
	trustNoRE     = re(`(?i)No,` + sp + `+(?:exit|quit)`)
	trustFocusRE  = re(`^` + sp + `*[❯›>]`)
	trustAnswered = []string{"Enter"}
)

// TrustAcceptKeys are the keys that answer a folder trust dialog with yes: arrows from the focused option to the yes
// option, then Enter. Nil when the text is not such a dialog or its focus cannot be read.
func TrustAcceptKeys(text string) []string {
	if MatchTrustPrompt(text) == nil {
		return nil
	}
	yes, focused, n := -1, -1, 0
	for _, line := range strings.Split(text, "\n") {
		isYes, isNo := trustYesRE.MatchString(line), trustNoRE.MatchString(line)
		if !isYes && !isNo {
			continue
		}
		if isYes && yes < 0 {
			yes = n
		}
		if trustFocusRE.MatchString(line) {
			focused = n
		}
		n++
	}
	if yes < 0 || focused < 0 {
		return nil
	}
	keys := []string{}
	for ; focused < yes; focused++ {
		keys = append(keys, "Down")
	}
	for ; focused > yes; focused-- {
		keys = append(keys, "Up")
	}
	return append(keys, trustAnswered...)
}
