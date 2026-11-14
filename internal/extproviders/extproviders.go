// Package extproviders keeps the external model providers a person added by API: a name, an address, the kind of API it
// speaks (OpenAI-compatible or Anthropic-compatible) and the name of the secret of the machine's safe that holds its
// key. The key itself is never kept here and never leaves in an answer: the registry file holds a reference, and the
// value is read from the safe only to make a request to the provider.
//
// A provider is no chat profile (internal/providers): those are the vendor setups an agent CLI runs on. A provider of
// this registry is what the background features call (internal/bgmodel); one that speaks the Anthropic API is marked
// as fit for Claude CLI, for the day a chat may pick it.
package extproviders

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agents"
)

// API is the protocol a provider speaks.
type API string

const (
	// APIOpenAI: GET <base>/models and POST <base>/chat/completions, the key as a Bearer token.
	APIOpenAI API = "openai"
	// APIAnthropic: GET <base>/v1/models and POST <base>/v1/messages, the key in x-api-key.
	APIAnthropic API = "anthropic"
)

// Valid says whether a is a protocol the registry knows.
func (a API) Valid() bool { return a == APIOpenAI || a == APIAnthropic }

// Limits of what a person may save.
const (
	MaxProviders = 32
	MaxName      = 60
	MaxURL       = 300
	MaxKey       = 4096
	MaxModels    = 500
	MaxModelID   = 200
	// MaxWindow bounds a model's context window (tokens).
	MaxWindow = 100 << 20
)

// Reasons of a refusal, as the page is told them.
const (
	ReasonBadName = "bad-name"
	// ReasonNameTaken: another account is called so already.
	ReasonNameTaken = "name-taken"
	ReasonBadURL    = "bad-url"
	ReasonBadAPI    = "bad-api"
	ReasonBadKey    = "bad-key"
	ReasonBadModel  = "bad-model"
	ReasonNotFound  = "not-found"
	ReasonTooMany   = "too-many"
	ReasonFailed    = "failed"
)

// Refusal is a reason the caller is told.
type Refusal struct {
	Reason string
	Err    error
}

func (r *Refusal) Error() string {
	if r.Err != nil {
		return "extproviders: " + r.Reason + ": " + r.Err.Error()
	}
	return "extproviders: " + r.Reason
}

func (r *Refusal) Unwrap() error { return r.Err }

func refuse(reason string) error { return &Refusal{Reason: reason} }

// ReasonOf is the reason of any error of the registry.
func ReasonOf(err error) string {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Reason
	}
	return ReasonFailed
}

// Model is one model of a provider: as the provider lists it, or as the person typed or corrected it.
type Model struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	// Efforts are the reasoning efforts the model takes (wire.Efforts, in their order); none: the model has no choice.
	Efforts []string `json:"efforts,omitempty"`
	// DefaultEffort is the one of Efforts a chat starts on; "" is the model's own.
	DefaultEffort string `json:"defaultEffort,omitempty"`
	// Window is the model's context window in tokens, as the provider lists it or the person typed it; 0: unknown
	// (Claude Code then keeps its own and compacts the chat late on a smaller window).
	Window int `json:"window,omitempty"`
	// Manual: the person added the model; a refresh from the provider keeps it even when the provider does not list it.
	Manual bool `json:"manual,omitempty"`
	// Edited: the person gave the model its name or efforts; a refresh keeps what they gave.
	Edited bool `json:"edited,omitempty"`
}

// Equal says whether two models are the same in every field.
func (m Model) Equal(o Model) bool {
	return m.ID == o.ID && m.Label == o.Label && slices.Equal(m.Efforts, o.Efforts) && m.DefaultEffort == o.DefaultEffort && m.Window == o.Window &&
		m.Manual == o.Manual && m.Edited == o.Edited
}

// ModelEfforts are the efforts a model may be given, in their order: those of the wire.
var ModelEfforts = []string{"low", "medium", "high", "xhigh", "max", "ultra"}

// MergeModels is a provider's fresh list with what the person did to the kept one: a model they named or gave efforts
// keeps their name and efforts, a model they added stays when the provider does not list it.
func MergeModels(kept, fresh []Model) []Model {
	byID := map[string]Model{}
	for _, m := range kept {
		byID[m.ID] = m
	}
	out := make([]Model, 0, len(fresh)+len(kept))
	seen := map[string]bool{}
	for _, f := range fresh {
		if k, ok := byID[f.ID]; ok && (k.Edited || k.Manual) {
			k.Manual = false
			k.Edited = true
			if k.Window == 0 {
				// The provider says the window the person did not give.
				k.Window = f.Window
			}
			f = k
		}
		seen[f.ID] = true
		out = append(out, f)
	}
	for _, k := range kept {
		if k.Manual && !seen[k.ID] && len(out) < MaxModels {
			out = append(out, k)
		}
	}
	return out
}

// Provider is one provider as the registry file keeps it.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	API     API    `json:"api"`
	// Preset is the template it was made from ("" for an address of the person's own).
	Preset string `json:"preset,omitempty"`
	// KeyRef names the secret of the safe that holds the key; "" for a provider that takes none (a local server).
	KeyRef string  `json:"keyRef,omitempty"`
	Models []Model `json:"models"`
	// ModelsAt is when the models were last taken from the provider (ms); 0 when they never were.
	ModelsAt int64 `json:"modelsAt,omitempty"`
	// Env are the variables the provider adds to the launch of an agent on it (launchenv.go).
	Env []EnvVar `json:"env,omitempty"`
	// Default marks the account of its provider that is used where no account is named: one of every Group.
	Default bool `json:"default,omitempty"`
	// Origin is the environment item the provider comes from; "" for one the person added.
	Origin    string `json:"origin,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Group is what the accounts of one provider share: the template they were made from, or the host of an address of
// the person's own. A person may add several accounts of a provider, each a record with a key of its own.
func (p Provider) Group() string {
	if p.Preset != "" && p.Preset != PresetCustom {
		return "preset:" + p.Preset
	}
	return "host:" + strings.ToLower(p.Host())
}

// settleDefaults leaves every group with one default account: the one marked first, else the one added first. A
// registry written before accounts had a default is read so, each provider the only account of its group.
func settleDefaults(list []Provider) {
	has := map[string]bool{}
	for i := range list {
		g := list[i].Group()
		if list[i].Default && has[g] {
			list[i].Default = false
		}
		has[g] = has[g] || list[i].Default
	}
	for i := range list {
		if g := list[i].Group(); !has[g] {
			list[i].Default, has[g] = true, true
		}
	}
}

// sameName says whether two names read the same to a person: the case and the spaces around do not tell them apart.
func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// freeName is name, or name with the first number that no other account has («DeepSeek 2»): two accounts are never
// called the same.
func freeName(list []Provider, name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(list, func(p Provider) bool { return sameName(p.Name, n) })
	}
	if !taken(name) {
		return name
	}
	for n := 2; ; n++ {
		if next := name + " " + strconv.Itoa(n); !taken(next) {
			return next
		}
	}
}

// View is a provider as the page sees it: everything but the name of its secret, and whether a key is saved.
type View struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	BaseURL  string  `json:"baseUrl"`
	Host     string  `json:"host"`
	API      API     `json:"api"`
	Preset   string  `json:"preset,omitempty"`
	HasKey   bool    `json:"hasKey"`
	Models   []Model `json:"models"`
	ModelsAt int64   `json:"modelsAt,omitempty"`
	// ClaudeCLI: Claude Code runs a chat on the provider; Proxied: through the runtime's proxy (an OpenAI-compatible
	// one), not straight to its address.
	ClaudeCLI bool `json:"claudeCli"`
	Proxied   bool `json:"proxied"`
	// Env are the variables of a launch on the provider; a secret one comes without its value.
	Env []EnvView `json:"env"`
	// Group is shared by the accounts of one provider; Default marks the one used where no account is named.
	Group   string `json:"group"`
	Default bool   `json:"default"`
	// Managed: the provider is the environment's; the page shows it and does not let it be changed or removed.
	Managed   bool  `json:"managed"`
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`
}

// ClaudeCLI says whether Claude Code runs a chat straight on the provider: it speaks the Anthropic API.
func (p Provider) ClaudeCLI() bool { return p.API == APIAnthropic }

// Proxied says whether Claude Code runs a chat on the provider through the runtime's proxy, which turns Anthropic's
// Messages into the provider's /chat/completions: it speaks the OpenAI API.
func (p Provider) Proxied() bool { return p.API == APIOpenAI }

// Host is the host of the provider's address.
func (p Provider) Host() string {
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// HasModel says whether the provider lists a model of that id.
func (p Provider) HasModel(id string) bool {
	return slices.ContainsFunc(p.Models, func(m Model) bool { return m.ID == id })
}

// Preset is a template of the form: a provider the person only has to give a key for.
type Preset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	API     API    `json:"api"`
	// KeyURL is the page of the vendor where a key is made.
	KeyURL string `json:"keyUrl,omitempty"`
	// Models are the models an account of the template starts with when the form names none: a vendor that speaks the
	// Anthropic API may list no models at its address.
	Models []Model `json:"models,omitempty"`
}

// PresetCustom is the template of an address of the person's own.
const PresetCustom = "custom"

var presets = []Preset{
	{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", API: APIOpenAI, KeyURL: "https://platform.deepseek.com/api_keys"},
	{ID: "groq", Name: "Groq", BaseURL: "https://api.groq.com/openai/v1", API: APIOpenAI, KeyURL: "https://console.groq.com/keys"},
	{ID: "xai", Name: "xAI", BaseURL: "https://api.x.ai/v1", API: APIOpenAI, KeyURL: "https://console.x.ai"},
	{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", API: APIOpenAI, KeyURL: "https://openrouter.ai/keys"},
	// The vendors Claude Code runs chats on by a key. A person adds as many accounts of one as they have keys, each
	// under a name of its own; the sign-in by a Claude subscription is not among them (one for the machine, the CLI's).
	{ID: "anthropic", Name: "Anthropic API", BaseURL: "https://api.anthropic.com", API: APIAnthropic, KeyURL: "https://console.anthropic.com/settings/keys"},
	{ID: "kimi-code", Name: "Kimi Code", BaseURL: "https://api.kimi.com/coding", API: APIAnthropic, KeyURL: "https://www.kimi.com/code",
		Models: []Model{{ID: "k3.1-pro", Label: "K3.1 Pro"}, {ID: "k3", Label: "K3"}, {ID: "kimi-for-coding", Label: "Kimi for Coding"}}},
	{ID: "kimi", Name: "Kimi · Moonshot", BaseURL: "https://api.moonshot.ai/anthropic", API: APIAnthropic, KeyURL: "https://platform.moonshot.ai/console/api-keys",
		Models: []Model{{ID: "kimi-k3.1-pro", Label: "K3.1 Pro"}, {ID: "kimi-k2.7-code", Label: "K2.7 Code"}}},
	{ID: "glm", Name: "GLM · Z.ai", BaseURL: "https://api.z.ai/api/anthropic", API: APIAnthropic, KeyURL: "https://z.ai/manage-apikey/apikey-list",
		Models: []Model{{ID: "glm-5.3", Label: "GLM 5.3"}, {ID: "glm-5.3-flash", Label: "GLM 5.3 Flash"}}},
	{ID: "glm-cn", Name: "GLM · bigmodel.cn", BaseURL: "https://open.bigmodel.cn/api/anthropic", API: APIAnthropic, KeyURL: "https://open.bigmodel.cn/usercenter/apikeys",
		Models: []Model{{ID: "glm-5.3", Label: "GLM 5.3"}, {ID: "glm-5.3-flash", Label: "GLM 5.3 Flash"}}},
	{ID: PresetCustom, Name: "Свой адрес", API: APIOpenAI},
}

// Presets are the templates of the form, in the order it lists them.
func Presets() []Preset { return slices.Clone(presets) }

func validPreset(id string) bool {
	return id == "" || slices.ContainsFunc(presets, func(p Preset) bool { return p.ID == id })
}

// CleanURL is a provider's address as it is kept: http or https, a host, no credentials, no query, no trailing slash.
func CleanURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > MaxURL {
		return "", refuse(ReasonBadURL)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", refuse(ReasonBadURL)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.EscapedPath(), "/"), nil
}

func cleanName(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || utf8.RuneCountInString(s) > MaxName {
		return "", refuse(ReasonBadName)
	}
	return s, nil
}

func cleanKey(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > MaxKey || strings.ContainsAny(s, "\r\n\x00") {
		return "", refuse(ReasonBadKey)
	}
	if agents.ClaudeSubscriptionToken(s) {
		return "", refuse(ReasonSubscriptionToken)
	}
	return s, nil
}

func cleanModels(in []Model) ([]Model, error) {
	if len(in) > MaxModels {
		return nil, refuse(ReasonBadModel)
	}
	out := make([]Model, 0, len(in))
	seen := map[string]bool{}
	for _, m := range in {
		m.ID = strings.TrimSpace(m.ID)
		m.Label = strings.TrimSpace(m.Label)
		if m.ID == "" || len(m.ID) > MaxModelID || len(m.Label) > MaxModelID || strings.ContainsAny(m.ID, " \t\r\n") {
			return nil, refuse(ReasonBadModel)
		}
		efforts, ok := cleanEfforts(m.Efforts)
		if !ok || m.DefaultEffort != "" && !slices.Contains(efforts, m.DefaultEffort) {
			return nil, refuse(ReasonBadModel)
		}
		m.Efforts = efforts
		if m.Window < 0 || m.Window > MaxWindow {
			return nil, refuse(ReasonBadModel)
		}
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out, nil
}

// cleanEfforts is a model's efforts in the wire's order, each once; ok is false for one the wire does not know.
func cleanEfforts(in []string) ([]string, bool) {
	if len(in) == 0 {
		return nil, true
	}
	for _, e := range in {
		if !slices.Contains(ModelEfforts, e) {
			return nil, false
		}
	}
	var out []string
	for _, e := range ModelEfforts {
		if slices.Contains(in, e) {
			out = append(out, e)
		}
	}
	return out, true
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(name string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 24 {
		s = strings.Trim(s[:24], "-")
	}
	return s
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Secrets is the part of the machine's safe the registry uses: the keys go there under the names it makes.
type Secrets interface {
	Put(name, value string) error
	Reveal(name string) (string, error)
	Remove(name string) error
	Has(name string) bool
}

// Store is the registry file of one data folder: <Dir>/providers.json, written whole beside and renamed over.
type Store struct {
	Dir string
	Now func() time.Time

	mu sync.Mutex
}

type fileBody struct {
	Providers []Provider `json:"providers"`
}

func (s *Store) path() string { return filepath.Join(s.Dir, "providers.json") }

func (s *Store) now() int64 {
	if s.Now != nil {
		return s.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

func (s *Store) read() (fileBody, error) {
	var body fileBody
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return body, nil
	}
	if err != nil {
		return body, err
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fileBody{}, err
	}
	for i := range body.Providers {
		if body.Providers[i].Models == nil {
			body.Providers[i].Models = []Model{}
		}
	}
	settleDefaults(body.Providers)
	return body, nil
}

func (s *Store) write(body fileBody) error {
	raw, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// List is the providers in the order they were added.
func (s *Store) List() ([]Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.read()
	return body.Providers, err
}

// Get is the provider of that id.
func (s *Store) Get(id string) (Provider, error) {
	list, err := s.List()
	if err != nil {
		return Provider{}, err
	}
	for _, p := range list {
		if p.ID == id {
			return p, nil
		}
	}
	return Provider{}, refuse(ReasonNotFound)
}

// change reads the file under the lock, lets edit change it and writes it back.
func (s *Store) change(edit func(*fileBody) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := s.read()
	if err != nil {
		return err
	}
	if err := edit(&body); err != nil {
		return err
	}
	return s.write(body)
}
