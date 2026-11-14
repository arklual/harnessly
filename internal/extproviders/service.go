package extproviders

import (
	"context"
	"net/url"
	"slices"
	"strings"
)

// sameServer says whether two addresses are of one server: the same scheme and host (with its port).
func sameServer(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	return errA == nil && errB == nil && ua.Host != "" && ua.Scheme == ub.Scheme && strings.EqualFold(ua.Host, ub.Host)
}

// Service is the registry with the safe its keys live in and the client that asks a provider for its models.
type Service struct {
	// Bound says whether the person allowed a secret of their safe for a site (scheme://host[:port]): a secret that is
	// only named, by an agent or by the environment, is used for the server it was allowed for and for no other. nil:
	// no secret is.
	Bound   func(name, origin string) bool
	Store   *Store
	Secrets Secrets
	// Probe asks a provider; nil is a Prober of its own.
	Probe *Prober
	// OnChange is told after the registry changed: what happened ("created", "updated", "removed") to which provider.
	OnChange func(what string, before, after *Provider)
}

func (s *Service) prober() *Prober {
	if s.Probe != nil {
		return s.Probe
	}
	return &Prober{}
}

func (s *Service) changed(what string, before, after *Provider) {
	if s.OnChange != nil {
		s.OnChange(what, before, after)
	}
}

// ViewOf is a provider as the page sees it.
func (s *Service) ViewOf(p Provider) View {
	models := p.Models
	if models == nil {
		models = []Model{}
	}
	return View{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Host: p.Host(), API: p.API, Preset: p.Preset,
		HasKey: s.holds(p.KeyRef), Models: models, ModelsAt: p.ModelsAt, ClaudeCLI: p.ClaudeCLI() || p.Proxied(), Proxied: p.Proxied(),
		Group: p.Group(), Default: p.Default, Managed: p.Origin != "", Env: s.envView(p.Env),
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// List is the providers as the page sees them.
func (s *Service) List() ([]View, error) {
	list, err := s.Store.List()
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(list))
	for _, p := range list {
		out = append(out, s.ViewOf(p))
	}
	return out, nil
}

// Get is one provider as the registry keeps it.
func (s *Service) Get(id string) (Provider, error) { return s.Store.Get(id) }

// Key is the provider's key from the safe; "" for a provider that takes none.
func (s *Service) Key(p Provider) (string, error) {
	if !s.holds(p.KeyRef) {
		return "", nil
	}
	return s.Secrets.Reveal(p.KeyRef)
}

// Input is the form of a new provider. Key may be empty: a local server takes none.
type Input struct {
	Name    string  `json:"name"`
	BaseURL string  `json:"baseUrl"`
	API     API     `json:"api"`
	Preset  string  `json:"preset"`
	Key     string  `json:"key"`
	Models  []Model `json:"models"`
}

// Create adds a provider and puts its key into the safe.
func (s *Service) Create(in Input) (View, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return View{}, err
	}
	base, err := CleanURL(in.BaseURL)
	if err != nil {
		return View{}, err
	}
	if !in.API.Valid() {
		return View{}, refuse(ReasonBadAPI)
	}
	if !validPreset(in.Preset) {
		in.Preset = ""
	}
	key, err := cleanKey(in.Key)
	if err != nil {
		return View{}, err
	}
	if len(in.Models) == 0 {
		// The template knows the models of a vendor whose address lists none.
		if k := slices.IndexFunc(presets, func(p Preset) bool { return p.ID == in.Preset }); k >= 0 {
			in.Models = slices.Clone(presets[k].Models)
		}
	}
	models, err := cleanModels(in.Models)
	if err != nil {
		return View{}, err
	}
	var made Provider
	err = s.Store.change(func(body *fileBody) error {
		if len(body.Providers) >= MaxProviders {
			return refuse(ReasonTooMany)
		}
		// Another account of a provider the person has already: it is told apart by a number until it is given a name.
		name = freeName(body.Providers, name)
		id := slug(name)
		taken := func(id string) bool {
			return slices.ContainsFunc(body.Providers, func(p Provider) bool { return p.ID == id })
		}
		if id == "" || taken(id) {
			stem := id
			if stem == "" {
				stem = "provider"
			}
			for id = stem + "-" + randomHex(3); taken(id); id = stem + "-" + randomHex(3) {
			}
		}
		now := s.Store.now()
		made = Provider{ID: id, Name: name, BaseURL: base, API: in.API, Preset: in.Preset, Models: models, CreatedAt: now, UpdatedAt: now}
		if key != "" {
			made.KeyRef = "provider-" + id + "-key"
			if err := s.Secrets.Put(made.KeyRef, key); err != nil {
				return &Refusal{Reason: ReasonFailed, Err: err}
			}
		}
		body.Providers = append(body.Providers, made)
		settleDefaults(body.Providers)
		made = body.Providers[len(body.Providers)-1]
		return nil
	})
	if err != nil {
		return View{}, err
	}
	s.changed("created", nil, &made)
	return s.ViewOf(made), nil
}

// Patch changes a provider: a field left nil stays as it is. An empty Key removes the saved key.
type Patch struct {
	Name    *string  `json:"name"`
	BaseURL *string  `json:"baseUrl"`
	API     *API     `json:"api"`
	Key     *string  `json:"key"`
	Models  *[]Model `json:"models"`
	// Env, when given, is the whole list of the launch's variables: one that is not in it is removed.
	Env *[]EnvInput `json:"env"`
	// Default true makes the account the default one of its provider; false is not a change (another is made so).
	Default *bool `json:"default"`
}

// Update changes a provider. A new address or kind of API drops the models taken from the old one, unless the patch
// brings its own.
func (s *Service) Update(id string, patch Patch) (View, error) {
	var before, after Provider
	err := s.Store.change(func(body *fileBody) error {
		i := slices.IndexFunc(body.Providers, func(p Provider) bool { return p.ID == id })
		if i < 0 {
			return refuse(ReasonNotFound)
		}
		p := body.Providers[i]
		before = p
		// A provider of the environment is changed in the hub. Which account of a provider is the main one is the
		// person's choice here too.
		if p.Origin != "" && (patch.Name != nil || patch.BaseURL != nil || patch.API != nil || patch.Key != nil || patch.Models != nil || patch.Env != nil) {
			return refuse(ReasonManaged)
		}
		moved := false
		if patch.Name != nil {
			name, err := cleanName(*patch.Name)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(body.Providers, func(x Provider) bool { return x.ID != id && sameName(x.Name, name) }) {
				return refuse(ReasonNameTaken)
			}
			p.Name = name
		}
		if patch.BaseURL != nil {
			base, err := CleanURL(*patch.BaseURL)
			if err != nil {
				return err
			}
			moved = moved || base != p.BaseURL
			p.BaseURL = base
		}
		if patch.API != nil {
			if !patch.API.Valid() {
				return refuse(ReasonBadAPI)
			}
			moved = moved || *patch.API != p.API
			p.API = *patch.API
		}
		if moved {
			p.Models, p.ModelsAt = []Model{}, 0
		}
		// A key is saved for one server. An address that moves to another server drops it, unless the patch brings the
		// key of the new one: the old key is never sent to a server the person did not give it to.
		if !sameServer(before.BaseURL, p.BaseURL) && patch.Key == nil && p.KeyRef != "" {
			if p.owns(p.KeyRef) {
				_ = s.Secrets.Remove(p.KeyRef)
			}
			p.KeyRef = ""
		}
		if patch.Models != nil {
			models, err := cleanModels(*patch.Models)
			if err != nil {
				return err
			}
			p.Models = models
		}
		if patch.Key != nil {
			key, err := cleanKey(*patch.Key)
			if err != nil {
				return err
			}
			switch {
			case key != "":
				// A key typed here is kept under the registry's own name: a secret that was only named is not overwritten.
				if !p.owns(p.KeyRef) {
					p.KeyRef = "provider-" + p.ID + "-key"
				}
				if err := s.Secrets.Put(p.KeyRef, key); err != nil {
					return &Refusal{Reason: ReasonFailed, Err: err}
				}
			case p.KeyRef != "":
				if p.owns(p.KeyRef) {
					_ = s.Secrets.Remove(p.KeyRef)
				}
				p.KeyRef = ""
			}
		}
		if patch.Env != nil {
			env, err := s.applyEnv(p.ID, p.Env, *patch.Env)
			if err != nil {
				return err
			}
			p.Env = env
		}
		p.UpdatedAt = s.Store.now()
		if patch.Default != nil && *patch.Default {
			for j := range body.Providers {
				if body.Providers[j].Group() == p.Group() {
					body.Providers[j].Default = false
				}
			}
			p.Default = true
		}
		body.Providers[i] = p
		// An address that moved to another provider joins its accounts: every group keeps one default.
		settleDefaults(body.Providers)
		after = body.Providers[i]
		return nil
	})
	if err != nil {
		return View{}, err
	}
	s.changed("updated", &before, &after)
	return s.ViewOf(after), nil
}

// Remove deletes a provider and its key.
func (s *Service) Remove(id string) error {
	var gone Provider
	err := s.Store.change(func(body *fileBody) error {
		i := slices.IndexFunc(body.Providers, func(p Provider) bool { return p.ID == id })
		if i < 0 {
			return refuse(ReasonNotFound)
		}
		gone = body.Providers[i]
		if gone.Origin != "" {
			return refuse(ReasonManaged)
		}
		body.Providers = slices.Delete(body.Providers, i, i+1)
		// The default account is gone: the one added first among the rest of its provider takes its place.
		settleDefaults(body.Providers)
		return nil
	})
	if err != nil {
		return err
	}
	// A secret that was only named (by an agent, or by the person) is the person's and stays in the safe.
	for _, ref := range gone.secretRefs() {
		if gone.owns(ref) {
			_ = s.Secrets.Remove(ref)
		}
	}
	s.changed("removed", &gone, nil)
	return nil
}

// Draft is a form that is not saved yet: «Проверить» before «Сохранить». With ID and no Key the saved key of that
// provider is used, so a person who edits an address need not type the key again.
type Draft struct {
	ID      string `json:"id"`
	BaseURL string `json:"baseUrl"`
	API     API    `json:"api"`
	Key     string `json:"key"`
}

// CheckDraft asks the address of a form for its models.
func (s *Service) CheckDraft(ctx context.Context, d Draft) (Check, error) {
	base, err := CleanURL(d.BaseURL)
	if err != nil {
		return Check{}, err
	}
	if !d.API.Valid() {
		return Check{}, refuse(ReasonBadAPI)
	}
	key, err := cleanKey(d.Key)
	if err != nil {
		return Check{}, err
	}
	if key == "" && d.ID != "" {
		p, err := s.Store.Get(d.ID)
		if err != nil {
			return Check{}, err
		}
		// The saved key goes only to the server it was saved for: a form that names another server is checked without
		// it, and the person types the key again.
		if sameServer(p.BaseURL, base) {
			if key, err = s.Key(p); err != nil {
				return Check{}, &Refusal{Reason: ReasonFailed, Err: err}
			}
		}
	}
	return s.prober().Check(ctx, base, d.API, key), nil
}

// Check asks a saved provider for its models and, when it answers, keeps them.
func (s *Service) Check(ctx context.Context, id string) (Check, error) {
	p, err := s.Store.Get(id)
	if err != nil {
		return Check{}, err
	}
	key, err := s.Key(p)
	if err != nil {
		return Check{}, &Refusal{Reason: ReasonFailed, Err: err}
	}
	got := s.prober().Check(ctx, p.BaseURL, p.API, key)
	if !got.OK || len(got.Models) == 0 {
		return got, nil
	}
	var before, after Provider
	err = s.Store.change(func(body *fileBody) error {
		i := slices.IndexFunc(body.Providers, func(x Provider) bool { return x.ID == id })
		if i < 0 {
			return refuse(ReasonNotFound)
		}
		before = body.Providers[i]
		body.Providers[i].Models, body.Providers[i].ModelsAt = MergeModels(body.Providers[i].Models, got.Models), s.Store.now()
		after = body.Providers[i]
		return nil
	})
	// Other models are a change of the provider like one the person made: what follows the registry (the profiles the
	// chats start on) hears of it at once.
	if err == nil && !slices.EqualFunc(before.Models, after.Models, Model.Equal) {
		s.changed("updated", &before, &after)
	}
	if err == nil {
		// The page shows what is kept: the provider's list with what the person did to it.
		got.Models = after.Models
	}
	return got, nil
}

// ModelList is the models of a provider and when they were taken from it.
type ModelList struct {
	Models []Model `json:"models"`
	At     int64   `json:"at,omitempty"`
	// Check is how the refresh went, when one was made.
	Check *Check `json:"check,omitempty"`
}

// Models is the models of a provider: the kept ones, or (refresh, or none kept yet) the provider's own answer. A
// provider that does not answer leaves the kept ones.
func (s *Service) Models(ctx context.Context, id string, refresh bool) (ModelList, error) {
	p, err := s.Store.Get(id)
	if err != nil {
		return ModelList{}, err
	}
	if !refresh && p.ModelsAt != 0 {
		return ModelList{Models: p.Models, At: p.ModelsAt}, nil
	}
	got, err := s.Check(ctx, id)
	if err != nil {
		return ModelList{}, err
	}
	if p, err = s.Store.Get(id); err != nil {
		return ModelList{}, err
	}
	return ModelList{Models: p.Models, At: p.ModelsAt, Check: &got}, nil
}

// DefaultOf is the default account of the provider that p is (or was) an account of; false when it has none left.
func (s *Service) DefaultOf(p Provider) (Provider, bool) {
	list, err := s.Store.List()
	if err != nil {
		return Provider{}, false
	}
	for _, x := range list {
		if x.Default && x.Group() == p.Group() {
			return x, true
		}
	}
	return Provider{}, false
}
