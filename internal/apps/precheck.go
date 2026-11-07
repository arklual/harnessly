package apps

// The check of a catalog entry before the app is downloaded: each refusal has its kind, so the page says it in
// its own words next to the entry instead of an install that fails with a text.

// Kinds of refusal.
const (
	// RefuseTrust: the app is of the trusted tier (it runs code on the machine), which a catalog never installs.
	RefuseTrust = "trust"
	// RefuseHost: this harnessly is outside the range the app wants; Detail is the range.
	RefuseHost = "host"
	// RefuseName: another installed app has the same plugin name; Detail names it.
	RefuseName = "name"
	// RefuseCommand: another installed app takes the same slash command; Detail is the command and the app.
	RefuseCommand = "command"
	// RefuseKind: another installed app adds the same document type; Detail is the type and the app.
	RefuseKind = "kind"
)

// Refusal is one reason an entry cannot be installed here.
type Refusal struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// Precheck is the answer for an entry: why it cannot be installed (none: it can), and the secrets the person will be
// asked for after the install.
type Precheck struct {
	Refusals []Refusal `json:"refusals"`
	Secrets  []string  `json:"secrets"`
	// Access is what the entry says the app reaches, for the install screen. The package may reach more (its
	// network, servers, plugin parts): the install grants only these keys and the rest waits for consent.
	Access []Access `json:"access"`
}

// describe keeps in the entry what a machine checks before it downloads the app.
func (e *CatalogEntry) describe(m *Manifest) {
	e.Harnessly, e.Command = m.Harnessly, m.Command()
	e.Kinds, e.Secrets = nil, nil
	if s, err := m.SinopiaOf(); err == nil {
		for _, k := range s.Kinds {
			e.Kinds = append(e.Kinds, k.ID)
		}
	}
	for _, sec := range m.Secrets {
		if !sec.Optional {
			e.Secrets = append(e.Secrets, sec.Name)
		}
	}
}

// PrecheckEntry checks an entry against this machine: the host's version and the apps installed here (the entry's own
// app, an update of it, is no conflict).
func PrecheckEntry(e CatalogEntry, host string, installed []Installed) Precheck {
	out := Precheck{Refusals: []Refusal{}, Secrets: nonNil(e.Secrets)}
	if e.Trust == TrustTrusted {
		out.Refusals = append(out.Refusals, Refusal{Code: RefuseTrust})
	}
	if e.Harnessly != "" && host != "" {
		if rng, err := ParseRange(e.Harnessly); err == nil && !rng.Allows(host) {
			out.Refusals = append(out.Refusals, Refusal{Code: RefuseHost, Detail: e.Harnessly})
		}
	}
	for _, other := range installed {
		if other.ID == e.ID {
			continue
		}
		if PluginName(other.ID) == PluginName(e.ID) {
			out.Refusals = append(out.Refusals, Refusal{Code: RefuseName, Detail: other.ID})
		}
		if other.Manifest == nil {
			continue
		}
		if e.Command != "" && other.Manifest.Command() == e.Command {
			out.Refusals = append(out.Refusals, Refusal{Code: RefuseCommand, Detail: e.Command + " " + other.ID})
		}
		if s, err := other.Manifest.SinopiaOf(); err == nil {
			for _, k := range s.Kinds {
				for _, want := range e.Kinds {
					if k.ID == want {
						out.Refusals = append(out.Refusals, Refusal{Code: RefuseKind, Detail: want + " " + other.ID})
					}
				}
			}
		}
	}
	return out
}

// Precheck puts this machine's check into each entry the page is given: the team's catalog and the public one.
func (r *Registry) Precheck(entries []*CatalogEntry, host string) {
	installed, err := r.ListAll()
	if err != nil {
		return
	}
	for _, e := range entries {
		c := PrecheckEntry(*e, host, installed)
		secrets := make([]Secret, 0, len(e.Secrets))
		for _, n := range e.Secrets {
			secrets = append(secrets, Secret{Name: n})
		}
		c.Access = AccessOf(&Manifest{Permissions: e.Permissions, Secrets: secrets}, nil)
		e.Check = &c
	}
}
