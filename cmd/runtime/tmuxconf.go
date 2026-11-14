package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// defaultTmuxConf is the tmux.conf a standalone runtime writes when none is named. The page's terminal is xterm over a
// tmux client, and the scrollback lives in tmux: without the mouse tmux keeps xterm in its alternate screen with no
// mouse reports, and xterm turns the wheel into ↑/↓ — the shell and Claude Code page through their input history
// instead of the output. With the mouse on, the wheel enters copy mode and scrolls the pane's history,
// unless the program asked for the mouse itself (vim, less, htop): then the wheel is its own.
const defaultTmuxConf = `# Written by the runtime because RUNTIME_TMUX_CONF names no file. It is never overwritten once edited.
# The page's terminals have tabs of their own: no status bar (host name and clock).
set -g status off

# The wheel scrolls the pane's history (copy mode, left when scrolled back to the bottom), or goes to the program
# that asked for the mouse. Keys are the page's: tmux never turns the wheel into arrows.
set -g mouse on
set -g history-limit 10000
bind -T root WheelUpPane if-shell -F -t = "#{?pane_in_mode,1,#{mouse_any_flag}}" "send-keys -M" "copy-mode -e"
bind -T root WheelDownPane send-keys -M
bind -T copy-mode WheelUpPane send-keys -X -N 3 scroll-up
bind -T copy-mode WheelDownPane send-keys -X -N 3 scroll-down
bind -T copy-mode-vi WheelUpPane send-keys -X -N 3 scroll-up
bind -T copy-mode-vi WheelDownPane send-keys -X -N 3 scroll-down

# A drag selects; its end copies to the page's clipboard (OSC 52) and leaves copy mode only at the bottom, so a
# selection in the history keeps the place.
set -s set-clipboard on
bind -T copy-mode MouseDragEnd1Pane if-shell -F "#{==:#{scroll_position},0}" "send-keys -X copy-pipe-and-cancel" "send-keys -X copy-pipe-no-clear"
bind -T copy-mode-vi MouseDragEnd1Pane if-shell -F "#{==:#{scroll_position},0}" "send-keys -X copy-pipe-and-cancel" "send-keys -X copy-pipe-no-clear"

# Copy mode in the terminal's own colours: the selection and the position are inverted, the position without the clock.
set -g mode-style reverse
set -gq copy-mode-position-format "#[align=right][#{scroll_position}/#{history_size}]"

# The page reads the window title to know the pane is scrolled back (and shows "Down"): hx-scroll:1 in copy mode.
set -g set-titles on
set -g set-titles-string "hx-scroll:#{pane_in_mode}"
`

// oldDefaultTmuxConfs are the defaults earlier runtimes wrote: such a file was never edited and takes the new one.
var oldDefaultTmuxConfs = []string{
	"# Written by the runtime because RUNTIME_TMUX_CONF names no file. It is never overwritten: edit it freely.\nset -g status off\n",
}

// tmuxConfigFile is RUNTIME_TMUX_CONF; set but empty, tmux's defaults. Unset on a standalone runtime, it is tmux.conf in
// the data directory, written with defaultTmuxConf when missing or still an earlier default; in front of the previous
// runtime, none.
func tmuxConfigFile(cfg gate.Config) string {
	if f, ok := os.LookupEnv("RUNTIME_TMUX_CONF"); ok || !cfg.Standalone() || cfg.DataDir == "" {
		return f
	}
	f := filepath.Join(cfg.DataDir, "tmux.conf")
	raw, err := os.ReadFile(f)
	missing := errors.Is(err, os.ErrNotExist)
	if !missing && (err != nil || !isOldDefault(string(raw))) {
		return f
	}
	if err := os.WriteFile(f, []byte(defaultTmuxConf), 0o644); err != nil {
		agent.Log("warn", "standalone: default tmux.conf not written", agent.F{K: "err", V: err.Error()})
		if missing {
			return ""
		}
	}
	return f
}

func isOldDefault(conf string) bool {
	for _, old := range oldDefaultTmuxConfs {
		if conf == old {
			return true
		}
	}
	return false
}
