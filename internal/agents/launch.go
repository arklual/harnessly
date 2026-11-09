package agents

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Launch is the model and reasoning effort a conversation starts with; nil means the CLI's own default ("Auto"). Same
// shape as catalog.Choice, so either converts to the other.
type Launch struct {
	Model  *string `json:"model"`
	Effort *string `json:"effort"`
	// Ultracode starts Claude Code in its ultracode mode, beside the effort (the `ultracode` settings key).
	Ultracode bool `json:"ultracode,omitempty"`
	// Fast starts the CLI in its fast mode: Claude Code's `fastMode` setting, Codex's `service_tier = "fast"`.
	Fast bool `json:"fast,omitempty"`
	// Permission is how the agent asks before it acts (wire.PermissionMode); "" leaves it to the machine's default
	// (the runtime's dangerouslySkipPermissions, then the CLI's own settings).
	Permission wire.PermissionMode `json:"permission,omitempty"`
	// Profile starts the agent on a backend profile (another vendor's models behind the CLI, see ProfileLauncher). It
	// is set when the conversation starts and stays on the tab, so every later launch of it (resume, fork, rewind)
	// goes to the same vendor. Empty: the CLI's own account.
	Profile string `json:"profile,omitempty"`
	// Apps are the installed apps on in the conversation (internal/apps): the launch gives the agent their plugins,
	// instructions and tool policy. They stay on the tab and change when the person turns one on or off; the agent
	// takes the change when it starts next.
	Apps []string `json:"apps,omitempty"`
	// Conversation is the chat this launch is for. The runtime sets it when it starts or resumes the agent, and it is not
	// kept on the tab: the apps' launch records it in the launch's own folder (internal/apps), where the guard's journal
	// reads which chat a launch belongs to. Empty when the runtime does not know the chat at that point.
	Conversation string `json:"-"`
}

// AppIDRe: an app id goes into file names and the launch line, so it stays this strict (internal/apps checks the same).
var AppIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39}$`)

// AppLauncher gives a launch with apps what it needs of them. Nil on a machine without apps: such a launch fails.
type AppLauncher interface {
	// Claude is what a Claude Code chat with the apps adds: settingsPath is the `--settings` file the launch would use
	// without them, which the apps' file extends.
	Claude(ids []string, settingsPath string) (ClaudeApps, error)
	// ClaudeFor is Claude for the launch of a known chat: the launch's record names it.
	ClaudeFor(conversation string, ids []string, settingsPath string) (ClaudeApps, error)
	// Codex is what a Codex chat with the apps adds: the same rules and wall as Claude's, given the way Codex takes
	// them (`-c` flags on the launch line, never a file the agent could edit).
	Codex(conversation string, ids []string) (CodexApps, error)
}

// CodexApps is what the apps of a Codex chat add to its launch.
type CodexApps struct {
	// Guard is the PreToolUse hook command line (`harnessly hook app-guard --policy <file> --agent codex`); it goes first
	// among the event's hooks.
	Guard string
	// Servers are the apps' MCP servers, each a `-c mcp_servers.<name>.*` set.
	Servers []CodexAppServer
	// Instructions are the apps' instructions and the list of their skills, joined; "" for none. They go with the
	// workspace's developer instructions.
	Instructions string
	// TokenFile holds the launch's own token; the CLI gets it in place of the person's in the variable the runtime's MCP
	// config names ("" keeps the person's, a test).
	TokenFile string
	// Env are `VAR='value'` shell words the CLI gets, Wrap the shell words it runs under (the wall); both "" for none.
	Env, Wrap string
}

// CodexAppServer is one MCP server of an app as Codex is told about it.
type CodexAppServer struct {
	// Name is the server's name in Codex's config (plugin_<plugin>_<server>, the name the rules read from a tool).
	Name string
	// URL is a remote server's address; Command, Args and Env a local one's process.
	URL, Command string
	Args         []string
	Env, Headers map[string]string
	// TokenHeaders are headers that carry the launch's token: they read it from the CLI's environment, so it is on no
	// command line.
	TokenHeaders []string
}

// ClaudeApps is what the apps of a Claude Code chat add to its launch.
type ClaudeApps struct {
	// Settings replaces the --settings file: the one the launch had, with the app-guard hook.
	Settings string
	// PluginDirs are the apps' packages, each loaded with --plugin-dir (skills, commands, .mcp.json).
	PluginDirs []string
	// PromptFile is the apps' instructions joined, appended to the system prompt; "" for none.
	PromptFile string
	// MCPConfig is a second --mcp-config file: the host's app tools as the server harnessly-app; "" for none.
	MCPConfig string
	// RuntimeMCPConfig replaces the runtime's own --mcp-config: the same server with the launch's token in place of
	// the person's; "" keeps the runtime's.
	RuntimeMCPConfig string
	// Wrap are shell words the CLI runs under (the operating system's wall around the apps, internal/apps/boundary.go),
	// put between the launch's environment and the CLI; "" runs it as it is.
	Wrap string
	// Env are `VAR='value'` shell words the CLI gets after the launch's own environment (the agent's token file,
	// HARNESSLY_CLI_TOKEN_FILE); "" for none.
	Env string
	// Retoken copies an MCP config of the runtime's (one that carries the person's token in its headers) with the
	// launch's token in its place, and gives the copy's path; nil when the launch has no token of its own.
	Retoken func(mcpConfigPath string) (string, error)
}

// ErrNoApps: a launch names apps and this machine has none.
var ErrNoApps = errors.New("Apps are not available on this machine")

// ProfileLauncher gives a launch on a backend profile what it needs of the profile: the Claude Code settings file that
// points the CLI at the vendor, or the arguments that point Codex at it. It never gives a key: the CLI reads it from a
// file the profile names.
type ProfileLauncher interface {
	// ClaudeSettings is the `--settings` file of a Claude Code chat on a profile and a model; hooksPath is the settings
	// file of harnessly's hooks, which the file extends.
	ClaudeSettings(profile, model, hooksPath string) (string, error)
	// Codex is what a Codex chat on a profile adds to the launch.
	Codex(profile string) (CodexProfile, error)
	// Model is the capability record the launch checks its effort against.
	Model(profile, model string) (wire.AgentModel, bool)
}

// CodexProfile is what a Codex chat on a profile adds to the launch.
type CodexProfile struct {
	// Args are the `-c` flags that make the profile Codex's model provider.
	Args []string
	// KeyEnv is the variable Codex reads the key from, KeyFile the file the key is in. The pane's shell fills the variable
	// from the file (`VAR="$(cat file)"`), so the key is not in the line that is typed.
	KeyEnv, KeyFile string
}

// Efforts are every reasoning level the CLIs know, weakest first.
var Efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "ultracode"}

// ModelIDRe: a model id is typed into a shell, so it stays this strict (aliases, full ids, the [1m] suffix).
var ModelIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}(\[1m\])?$`)

// ProfileModelIDRe is a model of a vendor profile: routed backends name their models «vendor/model»
// («zai/glm-5.3», «openrouter/moonshotai/Kimi-K2»), with capitals and an «:tag». Still no shell metacharacter: up to
// four parts of [A-Za-z0-9._:-] joined by «/», each starting with a letter or a digit, and every launch line
// single-quotes it (ShellQuote) or passes it as one argv entry. A catalog model (no profile) stays ModelIDRe.
var ProfileModelIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}(/[A-Za-z0-9][A-Za-z0-9._:-]{0,63}){0,3}(\[1m\])?$`)

// MaxProfileModelID caps a profile model id.
const MaxProfileModelID = 128

// IsProfileModelID is a model id a vendor profile may name.
func IsProfileModelID(s string) bool {
	return len(s) <= MaxProfileModelID && ProfileModelIDRe.MatchString(s)
}

// IsLaunchModelID is the model check of a launch: a profile's model may be namespaced, a catalog model may not.
func IsLaunchModelID(profile, model string) bool {
	if profile != "" {
		return IsProfileModelID(model)
	}
	return ModelIDRe.MatchString(model)
}

// IsKnownEffort is a level of Efforts.
func IsKnownEffort(e string) bool { return slices.Contains(Efforts, e) }

var (
	ErrLaunchModel   = errors.New("Invalid launch model")
	ErrLaunchEffort  = errors.New("Invalid launch effort")
	ErrLaunchProfile = errors.New("Invalid launch profile")
	ErrLaunchApps    = errors.New("Invalid launch apps")
	ErrLaunchMode    = errors.New("Invalid launch permission mode")
)

// ProfileIDRe: a profile id is typed into a shell too.
var ProfileIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// CheckLaunch re-checks a stored choice right before it becomes shell text: a bad value stops the launch.
func CheckLaunch(l *Launch) error {
	if l == nil {
		return nil
	}
	if l.Model != nil && !IsLaunchModelID(l.Profile, *l.Model) {
		return ErrLaunchModel
	}
	if l.Effort != nil && !IsKnownEffort(*l.Effort) {
		return ErrLaunchEffort
	}
	for _, id := range l.Apps {
		if !AppIDRe.MatchString(id) {
			return ErrLaunchApps
		}
	}
	if l.Permission != "" && !l.Permission.Known() {
		return ErrLaunchMode
	}
	if l.Profile != "" {
		// A profile needs a model and takes no ultracode nor fast mode (both are Anthropic's). Its launcher checks the
		// effort against this model's own list.
		if !ProfileIDRe.MatchString(l.Profile) || l.Model == nil || *l.Model == "" || l.Ultracode || l.Fast {
			return ErrLaunchProfile
		}
	}
	return nil
}

// ReadLaunchShape reads a choice from request or stored values by shape only (model, effort as decoded from JSON).
// nil, true when both are absent; false when one is malformed.
func ReadLaunchShape(model, effort any) (*Launch, bool) {
	return ReadProfileLaunchShape("", model, effort)
}

// ReadProfileLaunchShape is ReadLaunchShape of a launch on profile: its model may be namespaced (ProfileModelIDRe).
// The profile is not set on the result; the caller that knows it does.
func ReadProfileLaunchShape(profile string, model, effort any) (*Launch, bool) {
	if model == nil && effort == nil {
		return nil, true
	}
	l := &Launch{}
	if model != nil {
		s, ok := model.(string)
		if !ok || !IsLaunchModelID(profile, s) {
			return nil, false
		}
		l.Model = &s
	}
	if effort != nil {
		s, ok := effort.(string)
		if !ok || !IsKnownEffort(s) {
			return nil, false
		}
		l.Effort = &s
	}
	return l, true
}

// LauncherModel is the model of a launch as the Codex launcher script is told it. The script forwards `--model` to the
// runtime whatever it is, and the one a platform ships does not know of profiles, so a chat on a backend profile names
// the profile in front of the model («xai/grok-build-0.1»); SplitLauncherModel reads it back where the runtime builds
// Codex's arguments. A model on the CLI's own account is told as it is.
func LauncherModel(l *Launch) string {
	if l.Profile == "" {
		return *l.Model
	}
	return l.Profile + "/" + *l.Model
}

// SplitLauncherModel is the profile and the model of a model as the launcher script sent it; a plain model has no profile.
func SplitLauncherModel(s string) (profile, model string) {
	if i := strings.IndexByte(s, '/'); i > 0 && ProfileIDRe.MatchString(s[:i]) {
		return s[:i], s[i+1:]
	}
	return "", s
}

// ShellQuote single-quotes s for sh.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

const (
	// MaxInitialPrompt caps an initial prompt, in UTF-16 units.
	MaxInitialPrompt = 20_000
	// InlinePromptMax: a longer prompt is not typed into the shell (a 16 KB typed line stalled zsh) but read from a file.
	InlinePromptMax = 2_000
)

// InitialPromptText is the prompt as it is passed: trimmed and capped.
func InitialPromptText(prompt string) string {
	return jscompat.Slice(jscompat.Trim(prompt), MaxInitialPrompt)
}

// PromptFromFile is the shell argument that reads a prompt file once and deletes it.
func PromptFromFile(file string) string {
	q := ShellQuote(file)
	return `"$(cat -- ` + q + ` && rm -f -- ` + q + `)"`
}

// WithInitialPrompt appends the initial prompt to a launch line: `-- <arg>` for Claude, `--prompt <arg>` for the Codex
// launcher. promptFile, when set, is where WriteLongPrompt put a long prompt.
func WithInitialPrompt(provider, command, prompt, promptFile string) string {
	text := InitialPromptText(prompt)
	if text == "" {
		return command
	}
	arg := PrintfQuote(text)
	if promptFile != "" {
		arg = PromptFromFile(promptFile)
	}
	switch provider {
	case Claude:
		return command + " -- " + arg
	case Codex:
		return command + " --prompt " + arg
	}
	return command
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// WriteLongPrompt writes a prompt too long to type (over InlinePromptMax units of the text InitialPromptText gives) to
// a private file initial-<uuid>.txt in dir; "" when the prompt goes inline.
func WriteLongPrompt(dir, text string) (string, error) {
	if jscompat.Len(text) <= InlinePromptMax {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file := filepath.Join(dir, "initial-"+randomUUID()+".txt")
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		return "", err
	}
	return file, nil
}

// EnvVar is one entry of a launch's environment prefix.
type EnvVar struct{ Key, Value string }

// StartEnvLimit is the most entries a launch's environment prefix may have.
const StartEnvLimit = 8

var startEnvValue = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)

// FormatStartEnv renders `KEY='value' ` per entry, in order. Keys must be keyPrefix followed by 1-40 of [A-Z_]; values
// need no quoting. The error is the message for the caller (a 400).
func FormatStartEnv(env []EnvVar, keyPrefix string) (string, error) {
	if len(env) > StartEnvLimit {
		return "", errors.New(locale.Pick("Too many environment variables", "Слишком много переменных окружения"))
	}
	keyRe := regexp.MustCompile(`^` + regexp.QuoteMeta(keyPrefix) + `[A-Z_]{1,40}$`)
	var b strings.Builder
	for _, e := range env {
		if !keyRe.MatchString(e.Key) {
			return "", errors.New(locale.Pick("Invalid environment variable "+jscompat.Slice(e.Key, 48), "Недопустимая переменная окружения "+jscompat.Slice(e.Key, 48)))
		}
		// The value is not echoed back: it may be a session id.
		if !startEnvValue.MatchString(e.Value) {
			return "", errors.New(locale.Pick("Invalid value of variable "+e.Key, "Недопустимое значение переменной "+e.Key))
		}
		b.WriteString(e.Key + "='" + e.Value + "' ")
	}
	return b.String(), nil
}

// StartPromptFilesLimit is the most instruction files a launch may add.
const StartPromptFilesLimit = 4

func isInside(child, root string) bool {
	if child == root {
		return true
	}
	if !strings.HasSuffix(root, "/") {
		root += "/"
	}
	return strings.HasPrefix(child, root)
}

// ResolvePromptFiles checks instruction files for a launch: Claude only, at most StartPromptFilesLimit, each an
// absolute path that resolves (symlinks followed) to a regular file inside dataDir. dataDirDisplay names dataDir in
// the messages (`~/.<name>`). The error is the message for the caller (a 400).
func ResolvePromptFiles(provider string, files []string, dataDir, dataDirDisplay string) ([]string, error) {
	if len(files) == 0 {
		return []string{}, nil
	}
	if provider != Claude {
		return nil, errors.New(locale.Pick("Only Claude supports instruction files", "Файлы инструкций поддерживает только Claude"))
	}
	if len(files) > StartPromptFilesLimit {
		return nil, errors.New(locale.Pick("Too many instruction files", "Слишком много файлов инструкций"))
	}
	root, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		return nil, errors.New(locale.Pick("Folder "+dataDirDisplay+" is unavailable", "Каталог "+dataDirDisplay+" недоступен"))
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		var real string
		if filepath.IsAbs(f) {
			real, _ = filepath.EvalSymlinks(f)
			if real != "" {
				real, _ = filepath.Abs(real)
			}
		}
		ok := false
		if real != "" && isInside(real, root) {
			st, err := os.Stat(real)
			ok = err == nil && st.Mode().IsRegular()
		}
		if !ok {
			return nil, errors.New(locale.Pick("Instruction file not found in "+dataDirDisplay, "Файл инструкций не найден в "+dataDirDisplay))
		}
		out = append(out, real)
	}
	return out, nil
}

// notSpace is JavaScript's \S.
var notSpace = "[^" + jscompat.SpaceClass[1:]

var bareCommand = regexp.MustCompile(`^` + jscompat.SpaceClass + `*(` + notSpace + `+)(` + jscompat.SpaceClass + `(?s:.*))?$`)
var trailingNewlines = regexp.MustCompile(`[\r\n]+$`)

// BareAgentArgs: text sent to an agent tab that is a bare `claude …` (`codex …`) for the tab's own agent is replaced
// by the platform's launch line plus these arguments. False when the text is not such a command.
func BareAgentArgs(content, panelType string) (string, bool) {
	binary := ProviderOfPanel(panelType)
	if binary == "" {
		return "", false
	}
	line := trailingNewlines.ReplaceAllString(content, "")
	m := bareCommand.FindStringSubmatch(line)
	if m == nil || m[1] != binary {
		return "", false
	}
	return strings.TrimRightFunc(m[2], jscompat.IsSpace), true
}

// ActiveApps is what a launcher may say about the apps a launch names: which of them are installed and on now. A chat
// whose app was removed, or one started in safe mode, starts with what is left — with none left, as a chat
// with no apps at all, wall included.
type ActiveApps interface {
	Active(ids []string) []string
}

// WithActiveApps is the launch with only the apps the launcher says are on; the launch itself when it names none or
// the launcher cannot say.
func WithActiveApps(l *Launch, apps AppLauncher) *Launch {
	if l == nil || len(l.Apps) == 0 {
		return l
	}
	active, ok := apps.(ActiveApps)
	if !ok {
		return l
	}
	kept := active.Active(l.Apps)
	if len(kept) == len(l.Apps) {
		return l
	}
	out := *l
	out.Apps = kept
	return &out
}
