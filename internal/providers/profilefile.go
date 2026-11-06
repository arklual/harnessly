package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/pricing"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The profiles file adds model profiles to the built-in presets without a rebuild. A profile of the file with the id of
// a preset replaces it; any other is appended after the built-in ones. Without a file (or an adapter) nothing changes.
//
//	{"profiles":[{"id":"corp-llm","label":"Corp LLM","vendor":"glm","api":"anthropic",
//	  "base_url":"https://corp-llm-proxy.example.com/anthropic","key_ref":"corp-llm-key",
//	  "models":[{"id":"corp-large","label":"Large","window":200000,"price":{"input":3,"output":15}}]}]}
//
// The key is a reference to a secret of the safe (key_ref), never the value: a file that holds a key by mistake is not
// read at all, because a file like this is meant to be committed and shared.

// ProfileFile is the document of the profiles file and of the adapter's GET /v1/profiles.
type ProfileFile struct {
	Profiles []FileProfile `json:"profiles"`
}

// FileProfile is one profile of a ProfileFile.
type FileProfile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Vendor picks the mark the page draws: kimi, glm, grok or gpt. A profile of a vendor of its own takes the closest.
	Vendor string `json:"vendor"`
	// API is the format the endpoint speaks: «anthropic» (Claude Code) or «openai-responses» (Codex).
	API     string `json:"api"`
	BaseURL string `json:"base_url"`
	Hint    string `json:"hint,omitempty"`
	// KeyRef is the name of the secret in the safe that holds the key; empty while the person types the key in Settings.
	KeyRef string `json:"key_ref,omitempty"`
	// KeyEnv is the variable Codex reads the key from (api openai-responses only).
	KeyEnv string            `json:"key_env,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Small  string            `json:"small,omitempty"`
	Test   string            `json:"test,omitempty"`
	Models []FileModel       `json:"models"`
}

// FileModel is one model of a FileProfile: its limits and, optionally, its price.
type FileModel struct {
	ID            string     `json:"id"`
	Label         string     `json:"label,omitempty"`
	Hint          string     `json:"hint,omitempty"`
	Window        int        `json:"window,omitempty"`
	Efforts       []string   `json:"efforts,omitempty"`
	DefaultEffort string     `json:"default_effort,omitempty"`
	Price         *FilePrice `json:"price,omitempty"`
}

// FilePrice is dollars per million tokens.
type FilePrice struct {
	Input       float64  `json:"input"`
	Output      float64  `json:"output"`
	CachedInput *float64 `json:"cached_input,omitempty"`
}

var (
	extraMu sync.RWMutex
	extra   []Preset

	keyRefShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._/-]{0,63}$`)
	// A name that looks like a vendor's key is refused: the reference is meant to be a name.
	keyLooking = regexp.MustCompile(`^(sk-|xai-|AIza|ghp_|glpat-)|^[A-Za-z0-9]{32,}$`)
	envShape   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

// all is the built-in presets followed by the ones the profiles file added; a file profile replaces a built-in one of the
// same id in its place.
func all() []Preset {
	extraMu.RLock()
	defer extraMu.RUnlock()
	out := append([]Preset(nil), presets...)
	for _, x := range extra {
		if i := slices.IndexFunc(out, func(p Preset) bool { return p.ID == x.ID }); i >= 0 {
			out[i] = x
		} else {
			out = append(out, x)
		}
	}
	return out
}

// LoadProfileFile reads the file and adds its profiles; on any error nothing is added.
func LoadProfileFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return AddProfiles(raw)
}

// AddProfiles adds the profiles of a ProfileFile document (the bytes of a file or an adapter's answer).
func AddProfiles(doc []byte) error {
	var f ProfileFile
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return fmt.Errorf("profiles: %w", err)
	}
	var out []Preset
	var prices []priceRow
	seen := map[string]bool{}
	var errs []error
	for _, fp := range f.Profiles {
		if seen[fp.ID] {
			errs = append(errs, fmt.Errorf("profile %q: twice", fp.ID))
			continue
		}
		seen[fp.ID] = true
		p, rows, err := fp.preset()
		if err != nil {
			errs = append(errs, fmt.Errorf("profile %q: %w", fp.ID, err))
			continue
		}
		out = append(out, p)
		prices = append(prices, rows...)
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	extraMu.Lock()
	for _, p := range out {
		if i := slices.IndexFunc(extra, func(x Preset) bool { return x.ID == p.ID }); i >= 0 {
			extra[i] = p
		} else {
			extra = append(extra, p)
		}
	}
	extraMu.Unlock()
	for _, r := range prices {
		pricing.RegisterOpenAI(r.model, r.input, r.cached, r.output)
	}
	return nil
}

type priceRow struct {
	model                 string
	input, cached, output float64
}

// ProfilesCapability is what an adapter lists in its description to offer profiles; it answers GET /v1/profiles.
const (
	ProfilesProtocol   = "harnessly.model-profiles/v1"
	ProfilesCapability = "profiles"
)

// LoadAdapter asks an adapter of the extension protocol (internal/hub/ext) for its profiles and adds them. A failing
// adapter adds nothing, and the built-in presets stay.
func LoadAdapter(ctx context.Context, c *ext.Client) error {
	d, err := c.Describe(ctx, ProfilesProtocol)
	if err != nil {
		return err
	}
	if !d.Has(ProfilesCapability) {
		return errors.New("profiles: the adapter does not list the capability " + ProfilesCapability)
	}
	var raw json.RawMessage
	if err := c.Do(ctx, http.MethodGet, "/v1/profiles", nil, false, &raw); err != nil {
		return err
	}
	return AddProfiles(raw)
}

func (fp FileProfile) preset() (Preset, []priceRow, error) {
	if !wire.ValidProfileID(fp.ID) {
		return Preset{}, nil, errors.New("id: not a profile id")
	}
	if strings.TrimSpace(fp.Label) == "" {
		return Preset{}, nil, errors.New("label: empty")
	}
	vendor := wire.ProfileVendor(fp.Vendor)
	if !slices.Contains(wire.ProfileVendors, vendor) || vendor == wire.VendorAuto {
		return Preset{}, nil, errors.New("vendor: one of kimi, glm, grok, gpt")
	}
	p := Preset{ID: fp.ID, Label: fp.Label, Vendor: vendor, Hint: fp.Hint, KeyRef: fp.KeyRef, KeyEnv: fp.KeyEnv}
	switch fp.API {
	case "anthropic":
		p.Via = wire.ProviderClaude
	case "openai-responses":
		p.Via = wire.ProviderCodex
		if !envShape.MatchString(fp.KeyEnv) {
			return Preset{}, nil, errors.New("key_env: the variable Codex reads the key from, like CORP_LLM_KEY")
		}
	default:
		return Preset{}, nil, errors.New("api: anthropic or openai-responses")
	}
	u, err := url.Parse(fp.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Preset{}, nil, errors.New("base_url: an address without credentials, query or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return Preset{}, nil, errors.New("base_url: https (http only for localhost)")
	}
	p.BaseURL, p.Host = fp.BaseURL, u.Host
	if fp.KeyRef != "" && (!keyRefShape.MatchString(fp.KeyRef) || keyLooking.MatchString(fp.KeyRef)) {
		return Preset{}, nil, errors.New("key_ref: the name of a secret of the safe, not a key")
	}
	for k := range fp.Env {
		if !envShape.MatchString(k) || strings.EqualFold(k, "ANTHROPIC_API_KEY") || strings.EqualFold(k, "ANTHROPIC_AUTH_TOKEN") {
			return Preset{}, nil, fmt.Errorf("env: %q is not an allowed variable", k)
		}
	}
	p.Env = fp.Env
	if len(fp.Models) == 0 {
		return Preset{}, nil, errors.New("models: none")
	}
	var rows []priceRow
	for _, fm := range fp.Models {
		if fm.ID == "" || strings.ContainsAny(fm.ID, " \t\r\n'\"") {
			return Preset{}, nil, errors.New("models: an id without spaces and quotes")
		}
		m := Model{ID: fm.ID, Label: fm.Label, Hint: fm.Hint, Window: fm.Window}
		if m.Label == "" {
			m.Label = m.ID
		}
		if m.Window < 0 {
			return Preset{}, nil, fmt.Errorf("model %s: window below zero", fm.ID)
		}
		for _, e := range fm.Efforts {
			eff := wire.Effort(e)
			if !slices.Contains([]wire.Effort{wire.EffortLow, wire.EffortMedium, wire.EffortHigh, wire.EffortXHigh, wire.EffortMax}, eff) {
				return Preset{}, nil, fmt.Errorf("model %s: effort %q", fm.ID, e)
			}
			m.Efforts = append(m.Efforts, eff)
		}
		if fm.DefaultEffort != "" {
			if !slices.Contains(fm.Efforts, fm.DefaultEffort) {
				return Preset{}, nil, fmt.Errorf("model %s: default_effort is not among efforts", fm.ID)
			}
			m.DefaultEffort = wire.Effort(fm.DefaultEffort)
		}
		if pr := fm.Price; pr != nil {
			if pr.Input < 0 || pr.Output < 0 || (pr.CachedInput != nil && *pr.CachedInput < 0) {
				return Preset{}, nil, fmt.Errorf("model %s: a price below zero", fm.ID)
			}
			cached := pr.Input
			if pr.CachedInput != nil {
				cached = *pr.CachedInput
			}
			rows = append(rows, priceRow{fm.ID, pr.Input, cached, pr.Output})
		}
		p.Models = append(p.Models, m)
	}
	p.Small, p.Test = fp.Small, fp.Test
	if p.Small == "" {
		p.Small = p.Models[0].ID
	}
	if p.Test == "" {
		p.Test = p.Small
	}
	for _, id := range []string{p.Small, p.Test} {
		if _, ok := p.ModelOf(id); !ok {
			return Preset{}, nil, fmt.Errorf("small/test: %q is not among models", id)
		}
	}
	return p, rows, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CommandSecrets reads a secret of the safe by running a command: the line is split at spaces (no shell), «{name}» in
// it is replaced by the secret's name, and what the command prints is the value. Empty line, no resolver.
func CommandSecrets(line string) func(string) (string, error) {
	argv := strings.Fields(line)
	if len(argv) == 0 {
		return nil
	}
	return func(name string) (string, error) {
		args := make([]string, len(argv)-1)
		for i, a := range argv[1:] {
			args[i] = strings.ReplaceAll(a, "{name}", name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, argv[0], args...).Output()
		return string(out), err
	}
}
