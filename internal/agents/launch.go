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
	// Profile starts the agent on a backend profile (another vendor's models behind the CLI, see ProfileLauncher). It
	// is set when the conversation starts and stays on the tab, so every later launch of it (resume, fork, rewind)
	// goes to the same vendor. Empty: the CLI's own account.
	Profile string `json:"profile,omitempty"`
}

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

// IsKnownEffort is a level of Efforts.
func IsKnownEffort(e string) bool { return slices.Contains(Efforts, e) }

var (
	ErrLaunchModel   = errors.New("Invalid launch model")
	ErrLaunchEffort  = errors.New("Invalid launch effort")
	ErrLaunchProfile = errors.New("Invalid launch profile")
)

// ProfileIDRe: a profile id is typed into a shell too.
var ProfileIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// CheckLaunch re-checks a stored choice right before it becomes shell text: a bad value stops the launch.
func CheckLaunch(l *Launch) error {
	if l == nil {
		return nil
	}
	if l.Model != nil && !ModelIDRe.MatchString(*l.Model) {
		return ErrLaunchModel
	}
	if l.Effort != nil && !IsKnownEffort(*l.Effort) {
		return ErrLaunchEffort
	}
	if l.Profile != "" {
		// A profile needs a model and takes no ultracode. Its launcher checks the effort against this model's own list.
		if !ProfileIDRe.MatchString(l.Profile) || l.Model == nil || *l.Model == "" || l.Ultracode {
			return ErrLaunchProfile
		}
	}
	return nil
}

// ReadLaunchShape reads a choice from request or stored values by shape only (model, effort as decoded from JSON).
// nil, true when both are absent; false when one is malformed.
func ReadLaunchShape(model, effort any) (*Launch, bool) {
	if model == nil && effort == nil {
		return nil, true
	}
	l := &Launch{}
	if model != nil {
		s, ok := model.(string)
		if !ok || !ModelIDRe.MatchString(s) {
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

// ANSICQuote quotes s as $'…' so a multi-line prompt stays on one typed line (bash and zsh read it).
func ANSICQuote(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\'':
			b.WriteString(`\'`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("'")
	return b.String()
}

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
	arg := ANSICQuote(text)
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
