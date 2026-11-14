package extproviders

import (
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// A provider of the environment (docs/hub/environment.md, «Model providers»): the organization writes it once in the
// hub and every machine of the audience has it, beside the accounts the person added by hand. Its source never holds a
// secret: the key and the secret variables are named, by the name of a secret of the person's safe or by a reference
// to a secret the organization keeps on the safe's server (vault:<path>#<key>). The person cannot change or remove
// such a provider on the machine; it leaves with its item.

// Reasons of a refusal about a provider of the environment.
const (
	// ReasonManaged: the provider is the environment's and is changed in the hub, not on the machine.
	ReasonManaged = "managed"
	// ReasonBadSecretRef: a secret is named by neither a name of the safe nor a vault: reference.
	ReasonBadSecretRef = "bad-secret-ref"
	// ReasonSecretUnbound: a secret of the safe that is named was not allowed by the person for this server, or is the
	// organization's and not an agent's to name.
	ReasonSecretUnbound = "secret-unbound"
	// ReasonSecretInSource: a variable that is a credential by its name was given a value instead of a reference.
	ReasonSecretInSource = "secret-in-source"
)

// MaxManagedModels is how many models a source may list.
const MaxManagedModels = 64

// ManagedSource is the source of an environment item of kind provider, as the hub keeps it and the machine reads it.
type ManagedSource struct {
	API     API    `json:"api"`
	BaseURL string `json:"baseUrl"`
	// Preset is the known provider the form was started from; "" for an address typed by hand.
	Preset string `json:"preset,omitempty"`
	// Models are the models offered; none: the machine asks the provider.
	Models []string `json:"models,omitempty"`
	// Key names the secret with the provider's key; "" for a provider that takes none.
	Key string `json:"key,omitempty"`
	// Env are the plain variables of a launch, NAME=value.
	Env []string `json:"env,omitempty"`
	// SecretEnv are the secret variables of a launch, NAME=<the secret's name or reference>.
	SecretEnv []string `json:"secretEnv,omitempty"`
}

var (
	safeNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	vaultRefRe = regexp.MustCompile(`^vault:[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*(#[A-Za-z0-9._-]{1,128})?$`)
)

// redirecting are the variables that send a launch's requests, and so its key and token, somewhere else than the
// provider's address, or take away the check of who answers: a source that is not typed by the person on their own
// screen (an item of the environment, an account an agent writes) cannot set them. The address is the source's
// baseUrl, and the secrets are bound to that server.
var redirecting = []string{
	"ANTHROPIC_BEDROCK_BASE_URL", "ANTHROPIC_VERTEX_BASE_URL", "ANTHROPIC_FOUNDRY_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CODE_USE_FOUNDRY", "OPENAI_BASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "NODE_TLS_REJECT_UNAUTHORIZED",
	"NODE_USE_ENV_PROXY", "GLOBAL_AGENT_HTTP_PROXY", "GLOBAL_AGENT_HTTPS_PROXY",
}

// redirects says whether a variable of a source would send the launch to another server than base.
func redirects(name, value, base string) bool {
	up := strings.ToUpper(name)
	if up == "ANTHROPIC_BASE_URL" {
		// The same server by another path is the provider still; another server is not.
		return !sameServer(value, base)
	}
	return slices.Contains(redirecting, up)
}

// SharedRef says whether a secret is named by a reference to the safe's server, not by a name of the person's safe.
func SharedRef(ref string) bool { return strings.HasPrefix(ref, "vault:") }

// ValidSecretRef says whether s names a secret: a name of the safe, or vault:<path>#<key>.
func ValidSecretRef(s string) bool {
	if SharedRef(s) {
		return len(s) <= 640 && vaultRefRe.MatchString(s) && !slices.ContainsFunc(strings.Split(strings.TrimPrefix(strings.SplitN(s, "#", 2)[0], "vault:"), "/"),
			func(seg string) bool { return seg == "." || seg == ".." })
	}
	return safeNameRe.MatchString(s)
}

// ParseManagedSource reads and checks a source. The answer is in the canonical form: trimmed, without repeats.
func ParseManagedSource(raw json.RawMessage) (ManagedSource, error) {
	var src ManagedSource
	if err := json.Unmarshal(raw, &src); err != nil {
		return ManagedSource{}, refuse(ReasonFailed)
	}
	return src.Clean()
}

// Clean checks a source and returns it in the canonical form.
func (m ManagedSource) Clean() (ManagedSource, error) {
	if !m.API.Valid() {
		return ManagedSource{}, refuse(ReasonBadAPI)
	}
	base, err := CleanURL(m.BaseURL)
	if err != nil {
		return ManagedSource{}, err
	}
	if !validPreset(m.Preset) {
		return ManagedSource{}, refuse(ReasonBadAPI)
	}
	out := ManagedSource{API: m.API, BaseURL: base, Preset: m.Preset}
	if len(m.Models) > MaxManagedModels {
		return ManagedSource{}, refuse(ReasonBadModel)
	}
	list := make([]Model, 0, len(m.Models))
	for _, id := range m.Models {
		list = append(list, Model{ID: id})
	}
	models, err := cleanModels(list)
	if err != nil {
		return ManagedSource{}, err
	}
	for _, model := range models {
		out.Models = append(out.Models, model.ID)
	}
	if out.Key = strings.TrimSpace(m.Key); out.Key != "" && !ValidSecretRef(out.Key) {
		return ManagedSource{}, refuse(ReasonBadSecretRef)
	}
	if len(m.Env)+len(m.SecretEnv) > MaxEnv {
		return ManagedSource{}, refuse(ReasonBadEnv)
	}
	seen := map[string]bool{}
	pair := func(line string) (string, string, error) {
		name, value, ok := strings.Cut(line, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || !envName.MatchString(name) || seen[name] || value == "" || len(value) > MaxEnvValue || strings.ContainsAny(value, "\r\n\x00") {
			return "", "", refuse(ReasonBadEnv)
		}
		if EnvReserved(name) {
			return "", "", refuse(ReasonEnvReserved)
		}
		seen[name] = true
		return name, value, nil
	}
	for _, line := range m.Env {
		name, value, err := pair(line)
		if err != nil {
			return ManagedSource{}, err
		}
		if EnvAlwaysSecret(name) {
			return ManagedSource{}, refuse(ReasonSecretInSource)
		}
		if redirects(name, value, base) {
			return ManagedSource{}, refuse(ReasonEnvReserved)
		}
		out.Env = append(out.Env, name+"="+value)
	}
	for _, line := range m.SecretEnv {
		name, ref, err := pair(line)
		if err != nil {
			return ManagedSource{}, err
		}
		if !ValidSecretRef(ref) {
			return ManagedSource{}, refuse(ReasonBadSecretRef)
		}
		if redirects(name, "", base) {
			return ManagedSource{}, refuse(ReasonEnvReserved)
		}
		out.SecretEnv = append(out.SecretEnv, name+"="+ref)
	}
	return out, nil
}

// SecretRefs are the secrets a source names, each once, in the order they are named.
func (m ManagedSource) SecretRefs() []string {
	out := []string{}
	add := func(ref string) {
		if ref != "" && !slices.Contains(out, ref) {
			out = append(out, ref)
		}
	}
	add(m.Key)
	for _, line := range m.SecretEnv {
		_, ref, _ := strings.Cut(line, "=")
		add(ref)
	}
	return out
}

// managedID is the registry id of an environment item's provider.
func managedID(origin string) string {
	return "env-" + strings.Trim(notSlug.ReplaceAllString(strings.ToLower(origin), "-"), "-")
}

// provider is the registry's record of a source.
func (m ManagedSource) provider(origin, name string) Provider {
	p := Provider{ID: managedID(origin), Origin: origin, Name: name, BaseURL: m.BaseURL, API: m.API, Preset: m.Preset, KeyRef: m.Key,
		Models: []Model{}}
	for _, id := range m.Models {
		p.Models = append(p.Models, Model{ID: id})
	}
	for _, line := range m.Env {
		n, v, _ := strings.Cut(line, "=")
		p.Env = append(p.Env, EnvVar{Name: n, Value: v})
	}
	for _, line := range m.SecretEnv {
		n, ref, _ := strings.Cut(line, "=")
		p.Env = append(p.Env, EnvVar{Name: n, Secret: true, Ref: ref})
	}
	return p
}

// sameManaged says whether the registry already keeps the source as it is.
func sameManaged(have, want Provider) bool {
	return have.Origin == want.Origin && have.BaseURL == want.BaseURL && have.API == want.API && have.Preset == want.Preset &&
		have.KeyRef == want.KeyRef && slices.Equal(have.Env, want.Env) && (len(want.Models) == 0 || slices.EqualFunc(have.Models, want.Models, Model.Equal))
}

// OriginOf is the site of a provider's address as the safe binds a secret to it: scheme://host[:port].
func OriginOf(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// allowed says whether the person allowed a secret of their safe for the server of base.
func (s *Service) allowed(name, base string) bool {
	origin := OriginOf(base)
	return origin != "" && s.Bound != nil && s.Secrets.Has(name) && s.Bound(name, origin)
}

// withoutWaiting takes out of a record the secrets of the person's safe that are not allowed for its server yet, and
// says which: such a provider is kept without them, so that nothing goes to a server the person did not allow it for.
func (s *Service) withoutWaiting(p Provider) (Provider, []string) {
	var waiting []string
	wait := func(ref string) bool {
		if ref == "" || SharedRef(ref) || s.allowed(ref, p.BaseURL) {
			return false
		}
		if !slices.Contains(waiting, ref) {
			waiting = append(waiting, ref)
		}
		return true
	}
	if wait(p.KeyRef) {
		p.KeyRef = ""
	}
	p.Env = slices.DeleteFunc(slices.Clone(p.Env), func(v EnvVar) bool { return v.Secret && wait(v.Ref) })
	return p, waiting
}

// HasManaged says whether the registry has the provider of an environment item.
func (s *Service) HasManaged(origin string) bool {
	_, err := s.Store.Get(managedID(origin))
	return err == nil
}

// ApplyManaged puts the provider of an environment item into the registry, or brings it in line with the item. The
// name is the item's; when a person's account is called so already, the environment's one gets the next free name.
// changed is false when the registry had it as it is. waiting are the secrets of the person's safe the source names
// that the person has not allowed for this server (or does not have): the provider is kept without them until they do.
func (s *Service) ApplyManaged(origin, name string, src ManagedSource) (bool, []string, error) {
	src, err := src.Clean()
	if err != nil {
		return false, nil, err
	}
	name, err = cleanName(name)
	if err != nil {
		return false, nil, err
	}
	want, waiting := s.withoutWaiting(src.provider(origin, name))
	var before *Provider
	var after Provider
	changed := false
	err = s.Store.change(func(body *fileBody) error {
		i := slices.IndexFunc(body.Providers, func(p Provider) bool { return p.ID == want.ID })
		others := slices.DeleteFunc(slices.Clone(body.Providers), func(p Provider) bool { return p.ID == want.ID })
		want.Name = freeName(others, name)
		now := s.Store.now()
		if i < 0 {
			if len(body.Providers) >= MaxProviders {
				return refuse(ReasonTooMany)
			}
			want.CreatedAt, want.UpdatedAt = now, now
			body.Providers = append(body.Providers, want)
			i = len(body.Providers) - 1
		} else {
			have := body.Providers[i]
			if sameManaged(have, want) && have.Name == want.Name {
				return nil
			}
			before = &have
			want.CreatedAt, want.UpdatedAt, want.Default = have.CreatedAt, now, have.Default
			// The models the machine took from the provider stay while the server is the same and the item lists none.
			if len(want.Models) == 0 && sameServer(have.BaseURL, want.BaseURL) && have.API == want.API {
				want.Models, want.ModelsAt = have.Models, have.ModelsAt
			}
			body.Providers[i] = want
		}
		settleDefaults(body.Providers)
		after, changed = body.Providers[i], true
		return nil
	})
	if err != nil || !changed {
		return false, waiting, err
	}
	what := "created"
	if before != nil {
		what = "updated"
	}
	s.changed(what, before, &after)
	return true, waiting, nil
}

// PruneManaged takes off the providers of the environment whose items are not in keep and says which went. Their
// secrets are not the registry's to remove: they are the person's own, or the organization's.
func (s *Service) PruneManaged(keep []string) ([]string, error) {
	var gone []Provider
	err := s.Store.change(func(body *fileBody) error {
		body.Providers = slices.DeleteFunc(body.Providers, func(p Provider) bool {
			if p.Origin == "" || slices.Contains(keep, p.Origin) {
				return false
			}
			gone = append(gone, p)
			return true
		})
		if len(gone) > 0 {
			settleDefaults(body.Providers)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(gone))
	for i := range gone {
		ids = append(ids, gone[i].Origin)
		s.changed("removed", &gone[i], nil)
	}
	return ids, nil
}

// holds says whether a secret a provider names can be told to be there: a name of the safe is looked up, a reference
// to the server is taken on trust (it is read when a launch needs it).
func (s *Service) holds(ref string) bool {
	return ref != "" && (SharedRef(ref) || s.Secrets.Has(ref))
}
