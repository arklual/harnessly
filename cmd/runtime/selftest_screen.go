package main

import (
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/cliversion"
	"github.com/ly-solutions/harnessly/internal/typist"
)

// The screen's part of the self-test: no agent is started and no pane is made.
//
//	screen_reading  the reader of this release reads the reference screens it carries (real captures of the checked
//	                versions of the agent's CLI) the way it must. A failure is the release's fault: the rollout takes
//	                it back.
//	cli_version     the versions of the agents' CLIs on the machine against the table of checked ones. It never fails
//	                the self-test: an unchecked version is the CLI's vendor moving on, and it must not stop a rollout.
//	                The check says so in its detail, the machine's preflight carries the warning, and the typist is
//	                cautious meanwhile.
func screenChecks(misread []string, clis []cliversion.State) []machineCheck {
	reading := machineCheck{ID: "screen_reading", Title: "Экран агента: эталонные экраны распознаются", OK: len(misread) == 0}
	if !reading.OK {
		reading.Detail = strings.Join(misread, "; ")
	}
	version := machineCheck{ID: "cli_version", Title: "Версия CLI агента сверена со списком проверенных", OK: true}
	var parts []string
	for _, c := range clis {
		switch c.Verdict {
		case cliversion.Checked:
			parts = append(parts, c.Provider+" "+c.Version+" — проверена")
		case cliversion.Unchecked:
			parts = append(parts, c.Provider+" "+c.Version+" — не проверена, осторожный режим")
		}
	}
	version.Detail = strings.Join(parts, "; ")
	if version.Detail == "" {
		version.Detail = "версия ещё не прочитана"
	}
	return []machineCheck{reading, version}
}

// withScreenChecks adds the screen's part to the terminal's.
func withScreenChecks(checks []machineCheck) []machineCheck {
	return append(checks, screenChecks(typist.CheckGoldens(), cliversion.Seen())...)
}

// watchCLIVersions reads the versions of the installed CLIs once at the start (every preflight after it — a launch
// asks one — reads them again, at the cost of a stat while the binary is the same) and says of an unchecked one in
// the journal, one line per version.
func watchCLIVersions(d deps) {
	cliversion.OnUnchecked(func(provider, version string) {
		agent.Log("warn", "the agent's CLI is of a version the screen reading was not checked against; cautious mode",
			agent.F{K: "provider", V: provider}, agent.F{K: "version", V: version})
	})
	if d.Tools != nil {
		go d.Tools.Runtime()
	}
}
