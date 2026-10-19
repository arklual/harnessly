// Package hooksetup writes the files the coding agents call the runtime through: the port file, the status hook and
// status line scripts Claude Code runs, the Codex hook script, the `--settings` file with the hooks, the MCP config, the
// prompt editor Claude gets as VISUAL, and the Codex launcher. Running agents keep the paths and scripts they were
// started with, so the file names and the scripts' curl contract stay as they were; what names the product (the data
// folder as scripts spell it, header names, the MCP server, the editor) comes from Config.
package hooksetup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"harnessly/internal/agents"
	"harnessly/internal/agents/claude"
	"harnessly/internal/agents/codex"
	"harnessly/internal/nodejson"
)

// File names in the data folder; agents started earlier hold these paths.
const (
	PortFile       = "port"
	TokenFile      = "cli-token"
	StatusHookFile = "status-hook.sh"
	StatuslineFile = "statusline.sh"
	CodexHookFile  = "codex-hook.sh"
	HooksFile      = "hooks.json"
	MCPConfigFile  = "preview-mcp.json"
	LauncherFile   = "codex-launcher.js"
	HandoffDir     = "prompt-handoff"
	BinDir         = "bin"
)

// Config names the product's parts.
type Config struct {
	// DataDir is the runtime's data folder (absolute).
	DataDir string
	// ScriptDataDir is the data folder as the scripts spell it, read by the shell at run time ("$HOME/.<name>"): the
	// scripts do not follow an overridden data folder, as before.
	ScriptDataDir string
	// TokenHeaders carry the CLI token in the scripts' requests, the current name first.
	TokenHeaders []string
	// MCPServer is the runtime's MCP server name, MCPPath its route; its header is TokenHeaders[0].
	MCPServer string
	MCPPath   string
	// Allow are the permissions granted in the hook settings (the MCP server under its current and former names).
	Allow []string
	// EditorName is the prompt editor script's file name under bin/, EditorTitle the name its comment gives it.
	EditorName  string
	EditorTitle string
	// Editor and Handoff, when set, are the editor script's path and its hand-off folder instead of the data folder's
	// bin/<EditorName> and prompt-handoff.
	Editor, Handoff string
	// HumanEditorVar is the variable the editor finds the person's own editor in.
	HumanEditorVar string
	// LegacyTmuxSocket: a pane on this server keys its hand-off file by pane id alone, on any other by socket and id.
	LegacyTmuxSocket string
	// Codex is what the Codex launcher script needs; nil writes none.
	Codex *codex.LauncherScriptConfig
}

// Writer writes the files of one data folder.
type Writer struct {
	cfg Config

	mu        sync.Mutex
	editor    string
	editorErr error
	editorSet bool
}

// New writes into cfg.DataDir.
func New(cfg Config) *Writer { return &Writer{cfg: cfg} }

func (w *Writer) path(name ...string) string {
	return filepath.Join(append([]string{w.cfg.DataDir}, name...)...)
}

// Paths of the files.
func (w *Writer) PortPath() string       { return w.path(PortFile) }
func (w *Writer) TokenPath() string      { return w.path(TokenFile) }
func (w *Writer) StatusHookPath() string { return w.path(StatusHookFile) }
func (w *Writer) StatuslinePath() string { return w.path(StatuslineFile) }
func (w *Writer) CodexHookPath() string  { return w.path(CodexHookFile) }
func (w *Writer) HooksPath() string      { return w.path(HooksFile) }
func (w *Writer) MCPConfigPath() string  { return w.path(MCPConfigFile) }
func (w *Writer) LauncherPath() string   { return w.path(LauncherFile) }
func (w *Writer) HandoffDir() string {
	if w.cfg.Handoff != "" {
		return w.cfg.Handoff
	}
	return w.path(HandoffDir)
}
func (w *Writer) EditorPath() string {
	if w.cfg.Editor != "" {
		return w.cfg.Editor
	}
	return w.path(BinDir, w.cfg.EditorName)
}

func (c Config) headerArgs(indent string, quote func(string) string) string {
	var b strings.Builder
	for _, h := range c.TokenHeaders {
		b.WriteString(indent + "-H " + quote(h+": ${TOKEN}"))
	}
	return b.String()
}

// headerLines are the headers one per continued line, as the multi-line curl calls write them.
func (c Config) headerLines() string {
	lines := make([]string, len(c.TokenHeaders))
	for i, h := range c.TokenHeaders {
		lines[i] = "  -H " + dq(h+": ${TOKEN}")
	}
	return strings.Join(lines, " \\\n")
}

func dq(s string) string { return `"` + s + `"` }

func (c Config) preamble() string {
	return `PORT_FILE="` + c.ScriptDataDir + `/` + PortFile + `"
TOKEN_FILE="` + c.ScriptDataDir + `/` + TokenFile + `"
[ -f "$PORT_FILE" ] || exit 0
[ -f "$TOKEN_FILE" ] || exit 0
PORT=$(cat "$PORT_FILE")
TOKEN=$(cat "$TOKEN_FILE")
`
}

// StatusHookScript is what Claude's lifecycle hooks run (`sh <script> <event>`): it reports the event, the tmux
// session, a notification's type and when the hook fired, in the background, and returns at once (a hook Claude
// cancels on its timeout under load would leave the tab unaware of the turn).
func (c Config) StatusHookScript() []byte {
	return []byte(`#!/bin/sh
EVENT="${1:-poll}"
` + c.preamble() + `AT=$(date +%s%3N 2>/dev/null)
case "$AT" in ''|*[!0-9]*) AT="" ;; esac

NOTIFICATION_TYPE=""
if [ "$EVENT" = "notification" ]; then
  NOTIFICATION_TYPE=$(sed -n 's/.*"notification_type"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi

(
  SESSION=$(tmux display-message -p '#{session_name}' 2>/dev/null) || SESSION=""
  PAYLOAD="{\"event\":\"${EVENT}\",\"session\":\"${SESSION}\""
  if [ -n "$NOTIFICATION_TYPE" ]; then
    PAYLOAD="${PAYLOAD},\"notificationType\":\"${NOTIFICATION_TYPE}\""
  fi
  if [ -n "$AT" ]; then
    PAYLOAD="${PAYLOAD},\"at\":${AT}"
  fi
  PAYLOAD="${PAYLOAD}}"
  curl -s --max-time 30 -X POST -o /dev/null -H 'Content-Type: application/json'` + c.headerArgs(" ", dq) + ` -d "$PAYLOAD" "http://localhost:${PORT}/api/status/hook" 2>/dev/null
) </dev/null >/dev/null 2>&1 &
exit 0
`)
}

// StatuslineScript is Claude's status line command: Claude's JSON goes to the runtime, the answer is the line shown.
func (c Config) StatuslineScript() []byte {
	return []byte(`#!/bin/sh
` + c.preamble() + `curl -sf --max-time 2 -X POST \
  -H 'Content-Type: application/json' \
` + c.headerLines() + ` \
  --data-binary @- \
  "http://localhost:${PORT}/api/status/statusline" 2>/dev/null || exit 0
`)
}

// CodexHookScript is what Codex runs for every hook: the payload goes to the runtime as it is, with the tmux session.
func (c Config) CodexHookScript() []byte {
	return []byte(`#!/usr/bin/env bash
set -u
` + c.preamble() + `SESSION=$(tmux display-message -p '#{session_name}' 2>/dev/null) || SESSION=""

curl -sS -X POST -o /dev/null \
` + c.headerLines() + ` \
  -H "Content-Type: application/json" \
  --data-binary @- \
  "http://localhost:${PORT}/api/status/hook?provider=codex&tmuxSession=${SESSION}" 2>/dev/null || true
exit 0
`)
}

// EditorScript is the prompt editor Claude runs for Ctrl+G: a hand-off file the runtime left for the pane is
// appended to Claude's buffer, so the text counts as typed; without one the person's own editor opens.
func (c Config) EditorScript(handoffDir string) []byte {
	legacy := c.LegacyTmuxSocket
	if legacy == "" {
		legacy = "''"
	}
	return []byte(`#!/bin/sh
# ` + c.EditorTitle + `: Claude Code runs it for Ctrl+G. A hand-off file the runtime left for this pane is
# appended to the prompt, so the text arrives as typed and not as a paste; without one the person's editor opens.
# Pane ids repeat across tmux servers, so a pane off the old ` + legacy + ` server is keyed by its server too.
dir=` + agents.ShellQuote(handoffDir) + `
key=${TMUX_PANE#%}
socket=${TMUX%%,*}
socket=${socket##*/}
[ -n "$key" ] && [ -n "$socket" ] && [ "$socket" != ` + legacy + ` ] && key="$socket-$key"
if [ -n "$key" ] && mv "$dir/$key.txt" "$dir/$key.$$.taking" 2>/dev/null; then
  cat "$dir/$key.$$.taking" >> "$1"
  rm -f "$dir/$key.$$.taking"
  exit 0
fi
editor=${` + c.HumanEditorVar + `:-vi}
[ "$editor" = "$0" ] && editor=vi
exec sh -c "$editor \"\$@\"" sh "$@"
`)
}

// MCPConfig is the `claude --mcp-config` file: the runtime's MCP server on loopback with the token in its header.
func (c Config) MCPConfig(port int, token string) []byte {
	header := ""
	if len(c.TokenHeaders) > 0 {
		header = c.TokenHeaders[0]
	}
	q := func(s string) string { return string(nodejson.Marshal(s)) }
	return []byte(`{
  "mcpServers": {
    ` + q(c.MCPServer) + `: {
      "type": "http",
      "url": ` + q(codex.MCPURL(port, c.MCPPath)) + `,
      "headers": {
        ` + q(header) + `: ` + q(token) + `
      }
    }
  }
}
`)
}

// HookSettings is the `--settings` file Claude is launched with.
func (w *Writer) HookSettings() []byte {
	return claude.HookSettings(claude.HookSettingsConfig{StatusHook: w.StatusHookPath(), Statusline: w.StatuslinePath(), Allow: w.cfg.Allow})
}

// writeAtomic replaces path with content through a temporary file in the same folder, then gives it mode: a script an
// agent runs at this moment is never read half written, and the mode holds whatever the umask or an older file had.
func writeAtomic(path string, content []byte, mode os.FileMode) error {
	tmp := path + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// writeManaged rewrites a file only when its content differs (a file left alone keeps its mtime and mode).
func writeManaged(path string, content []byte, mode os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(content) {
		return nil
	}
	return writeAtomic(path, content, mode)
}

// writePrivate holds the token: narrowed to 0600 before it is compared, in case a copy was left wider.
func writePrivate(path string, content []byte) error {
	_ = os.Chmod(path, 0o600)
	return writeManaged(path, content, 0o600)
}

// Result reports what Ensure could not write without failing the start.
type Result struct {
	// CodexHookFailed: the Codex hook script could not be written; Codex launches report nothing (tell the person).
	CodexHookFailed bool
}

// Ensure writes the port file (always) and every script and settings file whose content changed, at runtime start.
func (w *Writer) Ensure(port int, token string) (Result, error) {
	var res Result
	if err := os.MkdirAll(w.cfg.DataDir, 0o700); err != nil {
		return res, err
	}
	if err := writeAtomic(w.PortPath(), []byte(strconv.Itoa(port)), 0o600); err != nil {
		return res, err
	}
	if err := writeManaged(w.StatusHookPath(), w.cfg.StatusHookScript(), 0o755); err != nil {
		return res, err
	}
	if err := writeManaged(w.StatuslinePath(), w.cfg.StatuslineScript(), 0o755); err != nil {
		return res, err
	}
	if err := writeManaged(w.CodexHookPath(), w.cfg.CodexHookScript(), 0o700); err != nil {
		res.CodexHookFailed = true
	}
	if err := writePrivate(w.MCPConfigPath(), w.cfg.MCPConfig(port, token)); err != nil {
		return res, err
	}
	return res, writeManaged(w.HooksPath(), w.HookSettings(), 0o600)
}

// RemovePortFile removes the port file at shutdown, so the scripts stop calling a runtime that is gone.
func (w *Writer) RemovePortFile() error {
	if err := os.Remove(w.PortPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// EnsureToken is the CLI token the scripts send: the token file's, or a new random one written to it (0600).
func (w *Writer) EnsureToken() (string, error) {
	if raw, err := os.ReadFile(w.TokenPath()); err == nil {
		if t := strings.TrimSpace(string(raw)); t != "" {
			return t, nil
		}
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:])
	if err := os.MkdirAll(w.cfg.DataDir, 0o700); err != nil {
		return token, err
	}
	return token, writeAtomic(w.TokenPath(), []byte(token), 0o600)
}

// EnsureEditor writes the prompt editor script (and its hand-off folder) once per process, replacing whatever an
// older release left, and returns its path; it fits claude.LaunchConfig.EditorScript. A failed write is tried again
// next time.
func (w *Writer) EnsureEditor() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.editorSet && w.editorErr == nil {
		return w.editor, nil
	}
	target := w.EditorPath()
	err := os.MkdirAll(filepath.Dir(target), 0o700)
	if err == nil {
		err = os.MkdirAll(w.HandoffDir(), 0o700)
	}
	if err == nil {
		err = writeAtomic(target, w.cfg.EditorScript(w.HandoffDir()), 0o700)
	}
	w.editor, w.editorErr, w.editorSet = target, err, true
	if err != nil {
		return "", err
	}
	return target, nil
}

// EditorEnv is the environment prefix giving Claude the prompt editor at path.
func (w *Writer) EditorEnv(path string) string { return claude.EditorEnv(w.cfg.HumanEditorVar, path) }

// CodexLauncher writes the Codex launcher script when it differs and returns its path.
func (w *Writer) CodexLauncher() (string, error) {
	if w.cfg.Codex == nil {
		return "", errors.New("no Codex launcher configured")
	}
	return w.LauncherPath(), codex.EnsureLauncher(w.LauncherPath(), codex.LauncherScript(*w.cfg.Codex))
}
