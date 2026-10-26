// Package providers holds the backend profiles: another vendor's models (Kimi, GLM, Grok) behind an agent CLI the
// machine already runs. A profile is no Provider: Kimi and GLM run through Claude Code on a settings file of their own,
// Grok through Codex with an `xai` provider. The presets here are data; the person picks one and gives a key, and never
// types an address, so a key cannot be pointed at a host the runtime does not know.
package providers

import "github.com/ly-solutions/harnessly/internal/wire"

// Route is the agent CLI a preset runs through.
type Route = wire.Provider

// Model is one model of a preset: the vendor's id (what `--model` takes), a short label for the picker, the size of
// its context window in tokens and the reasoning efforts the vendor accepts. DefaultEffort is empty when there is no
// effort to choose.
type Model struct {
	ID            string
	Label         string
	Hint          string
	Window        int
	Efforts       []wire.Effort
	DefaultEffort wire.Effort
}

// Preset is a vendor setup.
type Preset struct {
	ID     string
	Label  string
	Vendor wire.ProfileVendor
	Via    Route
	// BaseURL is where the CLI sends its requests: the Anthropic-compatible address for Claude Code, the OpenAI-
	// compatible `/v1` for Codex.
	BaseURL string
	// Host is BaseURL's host, shown on the Settings screen.
	Host string
	Hint string
	// Models are the vendor's models, the first being the one a new chat offers.
	Models []Model
	// Small is the model that stands in for Claude Code's «haiku» (background work), one of Models.
	Small string
	// Test is the cheapest model, the one a test of the connection asks.
	Test string
	// Env is added to the CLI's environment (Claude Code only).
	Env map[string]string
	// KeyEnv is the variable the key goes to for the CLI that takes it from the environment (Codex only).
	KeyEnv string
	// Relayed: Codex reaches the vendor through the runtime's Relay, which holds the credential and turns Codex's
	// requests into what the vendor takes. Without a relay on the launcher it goes straight to BaseURL with the key.
	Relayed bool
}

const million = 1 << 20

var (
	lowHighMax  = []wire.Effort{wire.EffortLow, wire.EffortHigh, wire.EffortMax}
	grokEfforts = []wire.Effort{wire.EffortLow, wire.EffortMedium, wire.EffortHigh, wire.EffortXHigh}
)

var presets = []Preset{
	{
		ID: "kimi", Label: "Kimi · платформа", Vendor: wire.VendorKimi, Via: wire.ProviderClaude,
		BaseURL: "https://api.moonshot.ai/anthropic", Host: "api.moonshot.ai", Hint: "Платформа Moonshot, оплата за токены",
		Models: []Model{
			{ID: "kimi-k3.1-pro", Label: "K3.1 Pro", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortMax},
			{ID: "kimi-k3.1-pro-highspeed", Label: "K3.1 Pro HS", Hint: "Быстрая версия", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortMax},
			{ID: "kimi-k2.7-code", Label: "K2.7 Code", Hint: "Для кода и фоновой работы", Window: 256 << 10},
			{ID: "kimi-k2.7-code-highspeed", Label: "K2.7 Code HS", Hint: "Быстрая версия", Window: 256 << 10},
		},
		Small: "kimi-k2.7-code", Test: "kimi-k2.7-code",
	},
	{
		ID: "kimi-code", Label: "Kimi Code", Vendor: wire.VendorKimi, Via: wire.ProviderClaude,
		BaseURL: "https://api.kimi.com/coding/", Host: "api.kimi.com", Hint: "Подписка Kimi Code",
		Models: []Model{
			{ID: "k3.1-pro", Label: "K3.1 Pro", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortHigh},
			{ID: "k3.1-pro-highspeed", Label: "K3.1 Pro HS", Hint: "Быстрая версия", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortHigh},
			{ID: "k3", Label: "K3", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortHigh},
			{ID: "k3-256k", Label: "K3 · 256K", Hint: "Меньше расход подписки", Window: 256 << 10, Efforts: lowHighMax, DefaultEffort: wire.EffortHigh},
			{ID: "kimi-for-coding", Label: "Kimi for Coding", Hint: "K2.8 Preview · для обычной разработки", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortMax},
			{ID: "kimi-for-coding-highspeed", Label: "K2.7 Code HS", Hint: "Быстрая версия", Window: 256 << 10},
		},
		Small: "kimi-for-coding", Test: "kimi-for-coding",
	},
	{
		ID: "glm", Label: "GLM · Z.ai", Vendor: wire.VendorGLM, Via: wire.ProviderClaude,
		BaseURL: "https://api.z.ai/api/anthropic", Host: "api.z.ai", Hint: "Ключ Z.ai: API или Coding Plan",
		Models: []Model{
			{ID: "glm-5.3", Label: "GLM 5.3", Hint: "Только текст, рассуждает всегда", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortMax},
			{ID: "glm-5.3-flash", Label: "GLM 5.3 Flash", Hint: "Быстрая версия", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortMax},
		},
		Small: "glm-5.3-flash", Test: "glm-5.3-flash",
		Env: map[string]string{"API_TIMEOUT_MS": "3000000"},
	},
	{
		ID: "glm-cn", Label: "GLM · bigmodel.cn", Vendor: wire.VendorGLM, Via: wire.ProviderClaude,
		BaseURL: "https://open.bigmodel.cn/api/anthropic", Host: "open.bigmodel.cn", Hint: "Китайская платформа Zhipu",
		Models: []Model{
			{ID: "glm-5.3", Label: "GLM 5.3", Hint: "Только текст, рассуждает всегда", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortMax},
			{ID: "glm-5.3-flash", Label: "GLM 5.3 Flash", Hint: "Быстрая версия", Window: million, Efforts: lowHighMax, DefaultEffort: wire.EffortMax},
		},
		Small: "glm-5.3-flash", Test: "glm-5.3-flash",
		Env: map[string]string{"API_TIMEOUT_MS": "3000000"},
	},
	{
		ID: "xai", Label: "Grok", Vendor: wire.VendorGrok, Via: wire.ProviderCodex,
		BaseURL: "https://api.x.ai/v1", Host: "api.x.ai", Hint: "Через Codex: ключ API xAI или вход Grok CLI по подписке",
		Models: []Model{
			{ID: "grok-build-0.1", Label: "Grok Build 0.1", Window: 256 << 10},
			{ID: "grok-4.7", Label: "Grok 4.7", Window: 500_000, Efforts: grokEfforts, DefaultEffort: wire.EffortHigh},
		},
		Small: "grok-build-0.1", Test: "grok-build-0.1",
		KeyEnv: "XAI_API_KEY", Relayed: true,
	},
}

// Presets are the vendor setups the runtime knows, in the order the Settings screen lists them.
func Presets() []Preset { return append([]Preset(nil), presets...) }

// Find is the preset with that id.
func Find(id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// ModelOf is the preset's model with that id.
func (p Preset) ModelOf(id string) (Model, bool) {
	for _, m := range p.Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// AgentModels are the preset's models as the wire lists them. Efforts and their defaults belong to the vendor model,
// not to the CLI it runs through: Kimi and GLM take low/high/max, Grok 4.7 low/medium/high/xhigh.
func (p Preset) AgentModels() []wire.AgentModel {
	out := make([]wire.AgentModel, len(p.Models))
	for i, m := range p.Models {
		out[i] = wire.AgentModel{ID: m.ID, Label: m.Label, Hint: m.Hint, Efforts: append([]wire.Effort{}, m.Efforts...)}
		if m.DefaultEffort != "" {
			def := m.DefaultEffort
			out[i].DefaultEffort = &def
		}
	}
	return out
}
