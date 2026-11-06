package apps

import (
	"slices"
	"strings"
)

// Claude's own permission rules for what the apps deny and confirm. The app-guard hook decides every call, but a hook
// that did not start (no harnessly on the agent's PATH, no memory) or ran past its timeout is a non-blocking error to
// Claude Code, and the call goes through. Rules in the chat's settings (permissions.deny, permissions.ask) are held by
// Claude itself, so they stand when the hook does not. The hook stays for what these cannot say (the masks of
// arguments Claude's syntax lacks, the journal, the reasons).
//
// Only a rule Claude's syntax can say exactly is written; the rest is left to the hook alone. Claude orders its rules
// deny, ask, allow, whatever their specificity, while the hook takes the most specific one: a rule that a narrower rule
// of a weaker decision of the same app overrides (confirm `Bash(git*)` with allow `Bash(git status*)`) is not written,
// or Claude would ask about what the app allows.

// claudeFileTools are the tools Claude's Edit(...) rule covers.
var claudeFileTools = []string{"Edit", "Write", "MultiEdit", "NotebookEdit"}

// ClaudeRules are the deny and ask rules for the apps' deny and confirm lists.
func ClaudeRules(list []SnapshotApp) (deny, ask []string) {
	for _, a := range list {
		if a.Tools == nil {
			continue
		}
		plugin := PluginName(a.ID)
		weakerThanDeny := append(slices.Clone(a.Tools.Confirm), a.Tools.Allow...)
		for _, r := range a.Tools.Deny {
			if shadowed(r, weakerThanDeny) {
				continue
			}
			deny = appendNew(deny, claudeRule(r, plugin)...)
		}
		for _, r := range a.Tools.Confirm {
			if shadowed(r, a.Tools.Allow) {
				continue
			}
			ask = appendNew(ask, claudeRule(r, plugin)...)
		}
	}
	return deny, ask
}

func appendNew(dst []string, add ...string) []string {
	for _, r := range add {
		if !slices.Contains(dst, r) {
			dst = append(dst, r)
		}
	}
	return dst
}

// shadowed says whether one of the weaker rules names narrower than rule: the hook would let that one win.
func shadowed(rule string, weaker []string) bool {
	for _, w := range weaker {
		if w != rule && covers(rule, w) {
			return true
		}
	}
	return false
}

// covers says whether everything rule w names is also named by rule r (w is the narrower).
func covers(r, w string) bool {
	switch r {
	case "*":
		return true
	case "mcp:*":
		return strings.Contains(w, ".") || strings.HasPrefix(w, "mcp")
	}
	rn, rp, rParen := cutRule(r)
	wn, wp, wParen := cutRule(w)
	switch {
	case rParen && wParen:
		return rn == wn && glob(rp, strings.TrimRight(wp, "*"))
	case !rParen && wParen:
		return rn == wn
	case rParen:
		return false
	}
	if strings.HasSuffix(r, "*") {
		return strings.HasPrefix(w, strings.TrimSuffix(r, "*"))
	}
	return false
}

func cutRule(rule string) (name, pattern string, paren bool) {
	name, arg, ok := strings.Cut(rule, "(")
	if !ok || !strings.HasSuffix(arg, ")") {
		return rule, "", false
	}
	return name, strings.TrimSuffix(arg, ")"), true
}

// claudeRule is a manifest rule in Claude Code's syntax; none when Claude's cannot say it.
func claudeRule(rule, plugin string) []string {
	if rule == "*" || rule == "mcp:*" {
		return nil
	}
	if name, pattern, paren := cutRule(rule); paren {
		switch {
		case name == "Bash":
			if pattern == "" {
				return nil
			}
			if prefix, ok := strings.CutSuffix(pattern, "*"); ok && !strings.Contains(prefix, "*") {
				prefix = strings.TrimSuffix(strings.TrimRight(prefix, " "), ":")
				if prefix == "" {
					return nil
				}
				return []string{"Bash(" + prefix + ":*)"}
			}
			return []string{"Bash(" + pattern + ")"}
		case name == "Read" || slices.Contains(claudeFileTools, name):
			// Claude's path rules are relative to the project; only a literal absolute path or ~ means the same.
			if strings.HasPrefix(pattern, "/") {
				pattern = "/" + pattern
			} else if !strings.HasPrefix(pattern, "~/") {
				return nil
			}
			if name != "Read" {
				name = "Edit"
			}
			return []string{name + "(" + pattern + ")"}
		}
		return nil
	}
	if strings.HasPrefix(rule, "mcp__") {
		return nil
	}
	server, tool, ok := strings.Cut(rule, ".")
	if !ok {
		if strings.Contains(rule, "*") {
			return nil
		}
		return []string{rule}
	}
	if server == "" || strings.Contains(server, "*") {
		return nil
	}
	// The server of the host's app.* tools is reached by the launch's own config; any other is the app's plugin server or
	// the person's.
	var servers []string
	if server == "app" {
		servers = []string{HostServer}
		tool = strings.ReplaceAll(tool, ".", "_")
	} else {
		servers = []string{"plugin_" + plugin + "_" + server, server}
	}
	var out []string
	for _, s := range servers {
		switch {
		case tool == "*":
			out = append(out, "mcp__"+s)
		case strings.Contains(tool, "*"):
			return nil
		default:
			out = append(out, "mcp__"+s+"__"+tool)
		}
	}
	return out
}
