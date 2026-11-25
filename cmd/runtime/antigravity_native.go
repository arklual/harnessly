package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/antigravity"
	"github.com/ly-solutions/harnessly/internal/catalog"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/typist"
)

// Native chats stay opt-in until the deployment has verified the official hooks,
// MCP configuration and input adapter. Image generation is independently selectable.
func nativeAntigravityEnabled() bool {
	return os.Getenv("RUNTIME_ANTIGRAVITY_NATIVE") == "on"
}

func nativeAntigravityBinary(home string) string {
	if binary := os.Getenv("RUNTIME_ANTIGRAVITY_BIN"); binary != "" {
		return binary
	}
	local := filepath.Join(home, ".local", "bin", "agy")
	if st, err := os.Stat(local); err == nil && st.Mode().IsRegular() {
		return local
	}
	return ""
}

func (a *coreAgents) antigravityOptions() catalog.LaunchOptions {
	r := a.runtime().Antigravity
	out := catalog.LaunchOptions{Provider: agents.Antigravity, Source: "official-cli", Models: []catalog.Model{}}
	listed := make([]antigravity.Model, 0, len(r.Models))
	for _, m := range r.Models {
		if a.agyInput == nil {
			// The terminal CLI takes the listed ids as they are.
			out.Models = append(out.Models, catalog.Model{ID: m.ID, Label: m.Label, Hint: m.ID, Efforts: []string{}})
			continue
		}
		listed = append(listed, antigravity.Model{ID: m.ID, Label: m.Label})
	}
	for _, f := range antigravity.Families(listed) {
		m := catalog.Model{ID: f.ID, Label: f.Label, Hint: f.ID, Efforts: []string{}}
		if len(f.Efforts) > 0 {
			m.Efforts = f.Efforts
			def := f.DefaultEffort
			m.DefaultEffort = &def
		}
		out.Models = append(out.Models, m)
	}
	if !r.LoggedIn {
		out.Warnings = append(out.Warnings, "Run the official agy acp --login; model discovery alone does not prove native chat eligibility")
	}
	return out
}

type nativeAntigravityStart struct{ generation, expected, bound string }
type nativeAntigravityStarts struct {
	mu             sync.Mutex
	byConversation map[string]nativeAntigravityStart
}

func (s *nativeAntigravityStarts) begin(conversation, expected string) (string, error) {
	if conversation == "" {
		return "", errors.New("Antigravity launch needs a bound Harnessly conversation")
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	generation := hex.EncodeToString(b[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byConversation == nil {
		s.byConversation = map[string]nativeAntigravityStart{}
	}
	s.byConversation[conversation] = nativeAntigravityStart{generation: generation, expected: expected, bound: expected}
	return generation, nil
}

func (s *nativeAntigravityStarts) accept(conversation, generation, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.byConversation[conversation]
	if !ok || generation == "" || generation != current.generation || !agents.IsUUID(id) {
		return false
	}
	if current.bound != "" && current.bound != id {
		return false
	}
	if current.expected != "" && current.expected != id {
		return false
	}
	current.bound = id
	s.byConversation[conversation] = current
	return true
}

func (a *coreAgents) antigravityLine(workspace, resume string, chosen *agents.Launch, files []string) (string, error) {
	if !nativeAntigravityEnabled() {
		return "", errors.New("Antigravity native chats require explicit setup and RUNTIME_ANTIGRAVITY_NATIVE=on")
	}
	launch := agents.Launch{}
	if chosen != nil {
		launch = *chosen
	}
	if launch.Conversation == "" && resume != "" && a.nativeConversation != nil {
		launch.Conversation = a.nativeConversation(resume)
	}
	cfg := a.antigravity
	var line string
	var err error
	bridged := a.agyInput != nil && launch.Conversation != "" && appCLIBin() != ""
	switch {
	case bridged:
		// The chat runs beside its ACP bridge: messages, stops, approvals and model changes go over its socket.
		line, err = a.bridgeLine(appCLIBin(), resume, &launch, files)
	case resume != "":
		line, err = cfg.ResumeCommand(resume, workspace, &launch)
	default:
		line, err = cfg.LaunchCommand(workspace, &launch, files)
	}
	if err != nil {
		return "", err
	}
	generation, err := a.nativeStarts.begin(launch.Conversation, resume)
	if err != nil {
		return "", err
	}
	prefix := "HARNESSLY_ANTIGRAVITY_CONVERSATION=" + agents.ShellQuote(launch.Conversation) + " HARNESSLY_ANTIGRAVITY_GENERATION=" + agents.ShellQuote(generation) + " "
	prefix += "HARNESSLY_ANTIGRAVITY_EXPECTED_UUID=" + agents.ShellQuote(resume) + " "
	if a.nativeToken == nil {
		return "", errors.New("Antigravity launch token issuer is not configured")
	}
	tokenFile, err := a.nativeToken(generation)
	if err != nil {
		return "", err
	}
	header := a.nativeCLIHeader
	if header == "" {
		header = "x-cli-token"
	}
	prefix += "HARNESSLY_URL='' HARNESSLY_SESSION='' HARNESSLY_CLI_HEADER=" + agents.ShellQuote(header) + " "
	if a.nativeDataDir != "" {
		prefix += "HARNESSLY_DATA_DIR=" + agents.ShellQuote(a.nativeDataDir) + " "
	}
	prefix += "HARNESSLY_CLI_TOKEN='' HARNESSLY_CLI_TOKEN_FILE=" + agents.ShellQuote(tokenFile) + " "
	if bridged {
		// The bridge takes the shell's place, so the pane's foreground is the agent and the agent is the pane's child; it
		// removes the launch's token file itself when it ends.
		return prefix + line, nil
	}
	cleanup := "trap " + agents.ShellQuote(`rm -f -- "$HARNESSLY_CLI_TOKEN_FILE"`) + " EXIT; " + line
	return prefix + "sh -c " + agents.ShellQuote(cleanup), nil
}

func nativeAntigravityTokenIssuer(dataDir string) func(string) (string, error) {
	return func(generation string) (string, error) {
		// Generation is generated locally, not a user-supplied file component.
		if len(generation) != 32 {
			return "", errors.New("invalid native launch generation")
		}
		if _, err := hex.DecodeString(generation); err != nil {
			return "", err
		}
		token, err := (&gate.CLIToken{DataDir: dataDir}).IssueAgent(generation, 30*24*time.Hour)
		if err != nil {
			return "", err
		}
		dir := filepath.Join(dataDir, "antigravity", "launch", generation)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		path := filepath.Join(dir, ".credentials.json")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", err
		}
		_, writeErr := file.WriteString(token + "\n")
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			os.Remove(path)
			if writeErr != nil {
				return "", writeErr
			}
			return "", closeErr
		}
		return path, nil
	}
}

// Authorization binds a root hook once to the active generation. Old launches and
// nested native sessions cannot rebind a chat, even with the same workspace/token.
func (c *core) authorizeAntigravityHook(session, conversation, generation string, payload antigravity.HookPayload) bool {
	if c.agents == nil || !nativeAntigravityEnabled() {
		return false
	}
	found, ok := c.FindTab(conversation)
	if !ok || found.Tab.SessionName != session || found.Tab.PanelType.Value() != agents.AntigravityPanel {
		return false
	}
	if found.Tab.Agent.AgentState != nil {
		st := found.Tab.Agent.AgentState
		if st.ProviderID != agents.Antigravity {
			return false
		}
		if st.SessionID != nil && *st.SessionID != "" && *st.SessionID != payload.SessionID {
			return false
		}
	}
	workspace, err := filepath.EvalSymlinks(found.Cwd)
	if err != nil {
		return false
	}
	matchesWorkspace := false
	for _, path := range payload.WorkspacePaths {
		if real, err := filepath.EvalSymlinks(path); err == nil && real == workspace {
			matchesWorkspace = true
		}
	}
	if !matchesWorkspace {
		return false
	}
	processMatches := false
	for _, pid := range proc.Descendants(c.cfg.Tmux.PanePID(session)) {
		environment, ok := proc.Environ(pid)
		if !ok || !proc.HasEnvEntry(environment, "HARNESSLY_ANTIGRAVITY_CONVERSATION="+conversation) || !proc.HasEnvEntry(environment, "HARNESSLY_ANTIGRAVITY_GENERATION="+generation) {
			continue
		}
		words, ok := proc.System().ArgvOf(pid)
		if !ok {
			continue
		}
		if len(words) > 0 && (antigravity.MatchesProcess(filepath.Base(words[0]), words[1:]) || (c.agents.antigravity.Launcher != "" && words[0] == c.agents.antigravity.Launcher)) {
			processMatches = true
			break
		}
	}
	if processMatches {
		// A runtime restart can recover only the same stored UUID and live foreground launch.
		_ = c.nativePaneOwner(session)
	}
	return processMatches && c.agents.nativeStarts.accept(conversation, generation, payload.SessionID)
}

type antigravityProvider struct{ det *antigravity.Detector }

func (p antigravityProvider) ID() string                         { return agents.Antigravity }
func (p antigravityProvider) Running(_ int, children []int) bool { return p.det.IsRunning(children) }
func (p antigravityProvider) Detect(_ int, children []int) agents.SessionInfo {
	return p.det.Detect(children, false)
}
func (p antigravityProvider) Snapshot(path string, _ bool) (agents.Snapshot, error) {
	snap, _ := antigravity.ReadSnapshot(path)
	return snap, nil
}
func (p antigravityProvider) HistoryStats(path string) (*agents.HistoryStats, error) {
	stats := antigravity.ReadHistoryStats(path)
	return &stats, nil
}
func (p antigravityProvider) ParsePaneTitle(string) *string { return nil }
func (p antigravityProvider) SessionIDFromPath(path string) string {
	return antigravity.SessionIDFromPath(path)
}
func (p antigravityProvider) ReadSessionID(t agents.TabAgent) string {
	if st := t.AgentState; st != nil && st.ProviderID == agents.Antigravity && st.SessionID != nil {
		return *st.SessionID
	}
	return ""
}
func (p antigravityProvider) ReadJSONLPath(t agents.TabAgent) string {
	if st := t.AgentState; st != nil && st.ProviderID == agents.Antigravity && st.JSONLPath != nil {
		return *st.JSONLPath
	}
	return ""
}
func (p antigravityProvider) ReadSummary(t agents.TabAgent) string {
	if st := t.AgentState; st != nil && st.ProviderID == agents.Antigravity && st.Summary != nil {
		return *st.Summary
	}
	return ""
}
func (p antigravityProvider) FindSession(id string) string {
	if s, ok := p.det.FindSession(id); ok {
		return s.JSONLPath
	}
	return ""
}
func (p antigravityProvider) TranscriptIn(_ string, id string) string { return p.FindSession(id) }
func (p antigravityProvider) RunsWithHooks([]int) (bool, bool)        { return false, false }

var _ status.Provider = antigravityProvider{}

func newNativeAntigravityDetector(home string) *antigravity.Detector {
	det := antigravity.NewDetector(home)
	det.Binary = nativeAntigravityBinary(home)
	det.Installed = func() bool {
		binary := det.Binary
		if binary == "" {
			binary = "agy"
		}
		_, err := exec.LookPath(binary)
		return err == nil
	}
	return det
}

func newAntigravityStatus(home string) status.Provider {
	return antigravityProvider{det: newNativeAntigravityDetector(home)}
}

type nativeOwnedPane struct {
	typist.Pane
	alive func() bool
}

func (p nativeOwnedPane) NativeAgentAlive() bool { return p.alive != nil && p.alive() }

func (c *core) nativePaneOwner(session string) bool {
	if c.agents == nil || !nativeAntigravityEnabled() {
		return false
	}
	_, _, tab, ok := c.cfg.Tmux.ParseSessionName(session)
	if !ok {
		return false
	}
	found, ok := c.FindTab(tab)
	if !ok || found.Tab.SessionName != session || found.Tab.PanelType.Value() != agents.AntigravityPanel {
		return false
	}
	c.agents.nativeStarts.mu.Lock()
	launch, ok := c.agents.nativeStarts.byConversation[tab]
	c.agents.nativeStarts.mu.Unlock()
	if !ok {
		if !c.recoverNativeStart(session, tab, found.Tab.Agent.AgentState) {
			return false
		}
		c.agents.nativeStarts.mu.Lock()
		launch, ok = c.agents.nativeStarts.byConversation[tab]
		c.agents.nativeStarts.mu.Unlock()
	}
	if !ok || launch.bound == "" {
		return false
	}
	st := found.Tab.Agent.AgentState
	if st == nil || st.ProviderID != agents.Antigravity || st.SessionID == nil || *st.SessionID != launch.bound {
		return false
	}
	name := filepath.Base(c.agents.antigravity.Launcher)
	if name == "." || name == "" {
		name = "agy"
	}
	foreground := c.cfg.Tmux.PaneCurrentCommand(session)
	short := name
	if len(short) > 15 {
		short = short[:15]
	}
	if foreground != name && foreground != short && foreground != "agy" && !bridgeForeground(foreground) {
		return false
	}
	for _, pid := range proc.Descendants(c.cfg.Tmux.PanePID(session)) {
		env, ok := proc.Environ(pid)
		if !ok || !proc.HasEnvEntry(env, "HARNESSLY_ANTIGRAVITY_CONVERSATION="+tab) || !proc.HasEnvEntry(env, "HARNESSLY_ANTIGRAVITY_GENERATION="+launch.generation) {
			continue
		}
		argv, ok := proc.System().ArgvOf(pid)
		if !ok || len(argv) == 0 {
			continue
		}
		if antigravity.MatchesProcess(argv[0], argv[1:]) || (c.agents.antigravity.Launcher != "" && argv[0] == c.agents.antigravity.Launcher) {
			return true
		}
	}
	return false
}

func (c *core) recoverNativeStart(session, conversation string, st *agents.AgentState) bool {
	if st == nil || st.ProviderID != agents.Antigravity || st.SessionID == nil || st.JSONLPath == nil || !antigravity.ValidateTranscriptPath(c.cfg.Home, *st.JSONLPath, *st.SessionID) {
		return false
	}
	name := filepath.Base(c.agents.antigravity.Launcher)
	if name == "." || name == "" {
		name = "agy"
	}
	short := name
	if len(short) > 15 {
		short = short[:15]
	}
	foreground := c.cfg.Tmux.PaneCurrentCommand(session)
	if foreground != name && foreground != short && foreground != "agy" && !bridgeForeground(foreground) {
		return false
	}
	for _, pid := range proc.Descendants(c.cfg.Tmux.PanePID(session)) {
		argv, ok := proc.System().ArgvOf(pid)
		if !ok || len(argv) == 0 || !(antigravity.MatchesProcess(argv[0], argv[1:]) || c.agents.antigravity.Launcher != "" && argv[0] == c.agents.antigravity.Launcher) {
			continue
		}
		env, ok := proc.Environ(pid)
		if !ok || !proc.HasEnvEntry(env, "HARNESSLY_ANTIGRAVITY_CONVERSATION="+conversation) {
			continue
		}
		generation := ""
		for _, entry := range env {
			if strings.HasPrefix(entry, "HARNESSLY_ANTIGRAVITY_GENERATION=") {
				generation = strings.TrimPrefix(entry, "HARNESSLY_ANTIGRAVITY_GENERATION=")
			}
		}
		if len(generation) != 32 {
			continue
		}
		if _, err := hex.DecodeString(generation); err != nil {
			continue
		}
		c.agents.nativeStarts.mu.Lock()
		if c.agents.nativeStarts.byConversation == nil {
			c.agents.nativeStarts.byConversation = map[string]nativeAntigravityStart{}
		}
		if _, exists := c.agents.nativeStarts.byConversation[conversation]; !exists {
			c.agents.nativeStarts.byConversation[conversation] = nativeAntigravityStart{generation: generation, expected: *st.SessionID, bound: *st.SessionID}
		}
		c.agents.nativeStarts.mu.Unlock()
		return true
	}
	return false
}

func antigravityLastActivity(home, id string) *float64 {
	meta, ok := antigravity.NewDetector(home).FindSession(id)
	if !ok {
		return nil
	}
	st, err := os.Stat(meta.JSONLPath)
	if err != nil {
		return nil
	}
	ms := float64(st.ModTime().UnixMilli())
	return &ms
}

// bridgeLine starts the chat's agent as `harnessly antigravity-acp`, the bridge to the official CLI's Agent Client
// Protocol, listening on the socket of the chat's tab.
func (a *coreAgents) bridgeLine(bin, resume string, l *agents.Launch, files []string) (string, error) {
	if len(files) != 0 || l.Fast || l.Ultracode || l.Profile != "" || len(l.Apps) != 0 {
		return "", antigravity.ErrUnsupported
	}
	socket := a.agyInput.socket(l.Conversation)
	if socket == "" {
		return "", errors.New("Antigravity bridge: no socket path for the chat")
	}
	agy := a.antigravity.Launcher
	if agy == "" {
		agy = "agy"
	}
	q := agents.ShellQuote
	words := []string{q(bin), "antigravity-acp", "--agy", q(agy), "--socket", q(socket), "--mcp", "harnessly"}
	if resume != "" {
		if !agents.IsUUID(resume) {
			return "", errors.New("Invalid Antigravity conversation UUID")
		}
		words = append(words, "--conversation", q(resume))
	}
	if l.Model != nil && *l.Model != "" {
		words = append(words, "--model", q(*l.Model))
	}
	if l.Effort != nil && *l.Effort != "" {
		words = append(words, "--effort", q(*l.Effort))
	}
	if l.Permission != "" {
		mode, ok := bridgeMode[l.Permission]
		if !ok {
			return "", agents.ErrLaunchMode
		}
		words = append(words, "--mode", mode)
	}
	env, err := agents.FormatStartEnv(a.antigravity.Env, "HARNESSLY_")
	if err != nil {
		return "", err
	}
	return a.antigravity.EnvPrefix + env + strings.Join(words, " "), nil
}

// bridgeForeground: the pane's foreground is the harnessly CLI, the ACP bridge a native chat runs as; the agent itself
// is its child, which the callers look for among the pane's processes.
func bridgeForeground(foreground string) bool {
	name := filepath.Base(appCLIBin())
	if name == "." || name == "" {
		name = "harnessly"
	}
	return foreground == name || len(name) > 15 && foreground == name[:15]
}
