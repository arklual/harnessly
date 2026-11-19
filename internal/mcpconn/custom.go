package mcpconn

// The person's own service: a connection that is not in the catalog (a company's internal API, a self-made
// MCP server, any service nobody listed) kept in the same file as the others, in the same shape, with the same id
// scheme `custom-<slug>` so that an app's manifest can name it and the page of the service can show it. An agent
// assembles one from an address, a document or an OpenAPI file (the hub's connection_add); the person's yes on a card
// makes it. Its keys are only ever in the safe (cred of the server): an agent names a secret of the safe, never a value.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// CustomPrefix starts the catalog id of every service of the person's own.
const CustomPrefix = "custom-"

// The kinds of a service of one's own.
const (
	CustomMCP   = "mcp"   // an MCP server by its address (http or sse; OAuth, a header or no sign-in)
	CustomAPI   = "api"   // an HTTP API with keys the runtime puts in
	CustomLocal = "local" // a command the runtime starts on this machine: someone else's code, only after a yes
)

var customIDRe = regexp.MustCompile(`^custom-[a-z0-9]+(-[a-z0-9]+)*$`)

// IsCustomID says whether id has the shape of a service of one's own (it may or may not exist).
func IsCustomID(id string) bool { return len(id) <= 48 && customIDRe.MatchString(id) }

// CustomMeta is what the person (or the agent for them) wrote about their service: the description its page shows and
// the group an app's `category` finds it in. Kept in the server's record.
type CustomMeta struct {
	// Category is one of the catalog's groups (tasks, docs, code, …); "utility" when none fits.
	Category Kind `json:"category"`
	// Letter is the one character the page draws as the service's mark.
	Letter  string   `json:"letter,omitempty"`
	About   string   `json:"about,omitempty"`
	AboutRu string   `json:"aboutRu,omitempty"`
	Can     []string `json:"can,omitempty"`
	CanRu   []string `json:"canRu,omitempty"`
	Steps   []string `json:"steps,omitempty"`
	StepsRu []string `json:"stepsRu,omitempty"`
	Links   []Link   `json:"links,omitempty"`
}

// CustomKey is one key (or setting) of an API or a local server of one's own.
type CustomKey struct {
	// Name is the variable the templates and the process know it by: A-Z, 0-9, _.
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	Hint  string `json:"hint,omitempty"`
	// Plain is a setting that is not a secret (a host, a region): it is shown on the page.
	Plain    bool `json:"plain,omitempty"`
	Optional bool `json:"optional,omitempty"`
	// Safe is the name of a secret already in the safe that the runtime copies in (an agent asked the person for it with
	// `harnessly secret ask`). It must start with the service's id.
	Safe string `json:"safe,omitempty"`
}

// CustomInput describes a service of one's own: what an agent proposes and the page's form sends.
type CustomInput struct {
	// ID is `custom-<slug>`: given, it changes that service; empty, it is made of Name.
	ID   string `json:"id,omitempty"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	// URL is the MCP server's address, or an API's base (it may hold {KEY}).
	URL string `json:"url,omitempty"`
	// Transport hints "sse" for a legacy MCP server; the probe decides.
	Transport Transport `json:"transport,omitempty"`
	// HeaderName is the header an MCP server takes its key in (default Authorization).
	HeaderName string `json:"headerName,omitempty"`
	// Keys: an API's and a local server's fields; for an MCP server with a header, the one key of the header.
	Keys []CustomKey `json:"keys,omitempty"`
	// APIHeaders, APIQuery: where an API's keys go: "{KEY}" in a value; "{BASIC:LOGIN:PASSWORD}" for basic auth.
	APIHeaders map[string]string `json:"apiHeaders,omitempty"`
	APIQuery   map[string]string `json:"apiQuery,omitempty"`
	// CheckPath is a GET that answers 2xx when the keys are right.
	CheckPath string `json:"checkPath,omitempty"`
	// Guide is what an agent reads to use an API: its main paths, a few lines.
	Guide string `json:"guide,omitempty"`
	// Command is a local server: `npx -y pkg@1.2.3 …` or `uvx pkg==1.2.3 …`, the version pinned.
	Command []string `json:"command,omitempty"`
	CustomMeta
}

// CustomPlan is a service of one's own checked and ready to be kept: what the card shows and what a yes makes.
type CustomPlan struct {
	In       CustomInput
	Existing *Server
	// Pairs are the lines of the card.
	Pairs []Pair
	// Checked says in a sentence what was tried and how it went (or why it was not).
	Checked string
}

// Pair is one line of a card: a name and its value.
type Pair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

var (
	keyNameRe   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,39}$`)
	npxPinRe    = regexp.MustCompile(`^(@[a-z0-9._~-]+/)?[a-z0-9._~-]+@\d+\.\d+\.\d+([-+][0-9A-Za-z.+-]+)?$`)
	uvxPinRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+(\[[A-Za-z0-9,_-]+\])?==\d+(\.\d+)*([-+.][0-9A-Za-z.+-]+)?$`)
	headerKeyRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	queryKeyRe  = regexp.MustCompile(`^[A-Za-z0-9_.\[\]-]{1,64}$`)
	secretish   = regexp.MustCompile(`(?i)auth|key|token|secret|passw|cookie|sign`)
)

// clean is s in one line of at most n runes (a line of a card, a name, a hint).
func clean(s string, n int) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)), " ")
	if utf8.RuneCountInString(s) > n {
		s = strings.TrimSpace(string([]rune(s)[:n-1])) + "…"
	}
	return s
}

// block is text of several lines (a description, a guide), cut at n runes.
func block(s string, n int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f {
			return r
		}
		return -1
	}, s))
	if utf8.RuneCountInString(s) > n {
		s = strings.TrimSpace(string([]rune(s)[:n-1])) + "…"
	}
	return s
}

func cleanList(in []string, max, n int) []string {
	var out []string
	for _, s := range in {
		if s = clean(s, n); s != "" && len(out) < max {
			out = append(out, s)
		}
	}
	return out
}

// normalize checks in and writes it the one way the record keeps it. It reaches nothing.
func (in CustomInput) normalize() (CustomInput, error) {
	bad := func(format string, a ...any) (CustomInput, error) {
		return CustomInput{}, fmt.Errorf("%w: %s", ErrBadRequest, fmt.Sprintf(format, a...))
	}
	in.Name = clean(in.Name, 60)
	if in.Name == "" {
		return bad("name is required")
	}
	switch in.Kind {
	case CustomMCP, CustomAPI, CustomLocal:
	default:
		return bad("kind is mcp, api or local")
	}
	if in.ID == "" {
		in.ID = CustomPrefix + slugOf(in.Name)
		if len(in.ID) > 48 {
			in.ID = strings.TrimRight(in.ID[:48], "-")
		}
	}
	if !IsCustomID(in.ID) {
		return bad("id is custom-<slug> (lowercase letters, digits, dashes)")
	}
	if KnownCatalogID(in.ID) {
		return bad("id %s is taken by the catalog", in.ID)
	}
	in.Keys = slices.Clone(in.Keys)
	seen := map[string]bool{}
	for i, k := range in.Keys {
		k.Name = strings.ToUpper(strings.TrimSpace(k.Name))
		if !keyNameRe.MatchString(k.Name) {
			return bad("key name %q is A-Z, 0-9 and _", k.Name)
		}
		if seen[k.Name] {
			return bad("key %s is listed twice", k.Name)
		}
		seen[k.Name] = true
		k.Label, k.Hint = clean(k.Label, 80), clean(k.Hint, 200)
		if k.Label == "" {
			k.Label = k.Name
		}
		if k.Safe = strings.TrimSpace(k.Safe); k.Safe != "" && !strings.HasPrefix(k.Safe, in.ID+"-") {
			return bad("the safe secret of %s must be named %s-<something>: ask for it with `harnessly secret ask %s-%s`", k.Name, in.ID, in.ID, strings.ToLower(strings.ReplaceAll(k.Name, "_", "-")))
		}
		in.Keys[i] = k
	}
	switch in.Kind {
	case CustomMCP:
		canon, _, err := canonical(in.URL)
		if err != nil {
			return bad("url: %v", err)
		}
		in.URL = canon
		if in.HeaderName = strings.TrimSpace(in.HeaderName); in.HeaderName != "" && !headerKeyRe.MatchString(in.HeaderName) {
			return bad("header name %q", in.HeaderName)
		}
		if len(in.Keys) > 1 {
			return bad("an MCP server takes one key: the value of its header")
		}
		if in.Transport != SSE {
			in.Transport = HTTP
		}
		in.APIHeaders, in.APIQuery, in.CheckPath, in.Command = nil, nil, "", nil
	case CustomAPI:
		probe := strings.TrimSpace(in.URL)
		for _, m := range placeholder.FindAllStringSubmatch(probe, -1) {
			if !seen[m[1]] {
				return bad("url names {%s}, which is not among the keys", m[1])
			}
		}
		if _, _, err := canonical(placeholder.ReplaceAllString(probe, "x")); err != nil {
			return bad("url: %v", err)
		}
		if strings.ContainsAny(probe, "?#") {
			return bad("url is the base address only: a parameter of the query goes in apiQuery")
		}
		in.URL = strings.TrimRight(probe, "/")
		for _, m := range []map[string]string{in.APIHeaders, in.APIQuery} {
			for k, v := range m {
				if !queryKeyRe.MatchString(k) || strings.ContainsAny(v, "\r\n\x00") {
					return bad("a header or query name is not plain: %q", k)
				}
				if !placeholder.MatchString(v) && !basicAuth.MatchString(v) && secretish.MatchString(k) {
					return bad("%s holds a literal value: a key is never written into the record, name a key field there as {NAME}", k)
				}
				for _, p := range placeholder.FindAllStringSubmatch(v, -1) {
					if !seen[p[1]] {
						return bad("%s names {%s}, which is not among the keys", k, p[1])
					}
				}
				for _, p := range basicAuth.FindAllStringSubmatch(v, -1) {
					if !seen[p[1]] || !seen[p[2]] {
						return bad("%s names a key of {BASIC:…} that is not among the keys", k)
					}
				}
			}
		}
		for _, k := range in.Keys {
			used := strings.Contains(in.URL, "{"+k.Name+"}")
			for _, m := range []map[string]string{in.APIHeaders, in.APIQuery} {
				for _, v := range m {
					used = used || strings.Contains(v, "{"+k.Name+"}") || strings.Contains(v, ":"+k.Name)
				}
			}
			if !used {
				return bad("key %s is not placed anywhere: say which header or query parameter takes it (apiHeaders, apiQuery)", k.Name)
			}
		}
		if in.CheckPath = strings.TrimSpace(in.CheckPath); in.CheckPath != "" && !strings.HasPrefix(in.CheckPath, "/") {
			return bad("checkPath starts with /")
		}
		in.Guide = block(in.Guide, 4000)
		in.HeaderName, in.Command = "", nil
		in.Transport = API
	case CustomLocal:
		if len(in.Command) < 2 || len(in.Command) > 24 {
			return bad("command is `npx -y <package>@<version> …` or `uvx <package>==<version> …`")
		}
		for _, a := range in.Command {
			if a == "" || strings.ContainsAny(a, "\r\n\x00") {
				return bad("command has an empty or multi-line argument")
			}
		}
		switch in.Command[0] {
		case "npx":
			if in.Command[1] != "-y" || len(in.Command) < 3 || !npxPinRe.MatchString(in.Command[2]) {
				return bad("npx runs `npx -y <package>@<exact version>`: the version is pinned, no tags like latest")
			}
		case "uvx":
			if !uvxPinRe.MatchString(in.Command[1]) {
				return bad("uvx runs `uvx <package>==<exact version>`")
			}
		default:
			return bad("only npx and uvx start a local server (a pinned version of a package); a Docker image or a script is not run")
		}
		in.URL, in.HeaderName, in.APIHeaders, in.APIQuery, in.CheckPath = "", "", nil, nil, ""
		in.Transport = Stdio
	}
	in.Guide = block(in.Guide, 4000)
	// The description.
	in.About, in.AboutRu = block(in.About, 1500), block(in.AboutRu, 1500)
	in.Can, in.CanRu = cleanList(in.Can, 6, 140), cleanList(in.CanRu, 6, 140)
	in.Steps, in.StepsRu = cleanList(in.Steps, 8, 240), cleanList(in.StepsRu, 8, 240)
	if !KnownKind(string(in.Category)) {
		in.Category = KindUtility
	}
	if l := []rune(strings.TrimSpace(in.Letter)); len(l) > 0 && (l[0] >= '0' && l[0] <= '9' || l[0] >= 'A' && l[0] <= 'z' || l[0] > 0x7f) {
		in.Letter = strings.ToUpper(string(l[0]))
	} else {
		in.Letter = strings.ToUpper(string([]rune(in.Name)[:1]))
	}
	var links []Link
	for _, l := range in.Links {
		u, err := url.Parse(strings.TrimSpace(l.URL))
		if err != nil || u.Host == "" || u.Scheme != "https" && u.Scheme != "http" || len(links) >= 6 {
			continue
		}
		if !slices.Contains([]string{LinkDocs, LinkSetup, LinkToken, LinkPricing, LinkHome}, l.Kind) {
			l.Kind = LinkDocs
		}
		links = append(links, Link{Kind: l.Kind, URL: u.String()})
	}
	in.Links = links
	return in, nil
}

// entry is the catalog entry the page and the hub read a service of one's own through.
func (c CustomMeta) entry(srv Server) CatalogEntry {
	about := c.About
	blurb := about
	if i := strings.IndexAny(blurb, ".\n"); i > 20 {
		blurb = blurb[:i]
	}
	blurb = clean(blurb, 200)
	if blurb == "" {
		blurb = srv.Name
	}
	blurbRu := clean(strings.SplitN(c.AboutRu, "\n", 2)[0], 200)
	if blurbRu == "" {
		blurbRu = blurb
	}
	// What an agent searches by: the description and what the service can do.
	blurb += " " + strings.Join(c.Can, "; ")
	blurbRu += " " + strings.Join(c.CanRu, "; ")
	e := CatalogEntry{ID: srv.CatalogID, Name: srv.Name, Kind: c.Category, URL: srv.URL, Transport: srv.Transport,
		Auth: srv.Auth, HeaderName: srv.HeaderName, Command: srv.Command, Env: srv.Env,
		APIHeaders: srv.APIHeaders, APIQuery: srv.APIQuery, CheckPath: srv.CheckPath, Guide: srv.Guide, FixedEnv: srv.FixedEnv,
		Blurb: strings.TrimSpace(blurb), BlurbRu: strings.TrimSpace(blurbRu),
		About: c.About, AboutRu: c.AboutRu, Can: c.Can, CanRu: c.CanRu, Steps: c.Steps, StepsRu: c.StepsRu, Links: c.Links}
	if len(srv.Command) > 0 {
		e.Runtime = srv.Command[0]
	}
	return e
}

// entryOf is the catalog entry of srv: the catalog's own, or the one a service of one's own makes.
func entryOf(srv Server) (CatalogEntry, bool) {
	if e, ok := catalogEntry(srv.CatalogID); ok {
		return e, true
	}
	if srv.Custom != nil {
		return srv.Custom.entry(srv), true
	}
	return CatalogEntry{}, false
}

// KnownConnectionID says whether id can name a connection in an app's manifest: a catalog entry, or a service of the
// person's own (its shape: the service itself is the person's and may be made later).
func KnownConnectionID(id string) bool { return KnownCatalogID(id) || IsCustomID(id) }

var (
	customKindsMu sync.Mutex
	customKinds   = map[string]string{}
)

// rememberCustomKinds keeps the group of every service of one's own as the file says, for CatalogKind: the apps ask by id.
func rememberCustomKinds(list []Server) {
	customKindsMu.Lock()
	defer customKindsMu.Unlock()
	for _, srv := range list {
		if srv.Custom != nil {
			customKinds[srv.CatalogID] = string(srv.Custom.Category)
		}
	}
}

func customKindOf(id string) string {
	customKindsMu.Lock()
	defer customKindsMu.Unlock()
	return customKinds[id]
}

// KindOf is the group of srv (mail, tasks, …): the catalog's for an entry of it, the person's own choice for theirs.
func KindOf(srv Server) string {
	if e, ok := entryOf(srv); ok {
		return string(e.Kind)
	}
	return ""
}

// Custom is the service of one's own with id, if it exists.
func (s *Service) Custom(id string) (Server, bool) {
	list, err := s.List()
	if err != nil {
		return Server{}, false
	}
	for _, srv := range list {
		if srv.CatalogID == id && srv.Owner == "" && srv.Custom != nil {
			return srv, true
		}
	}
	return Server{}, false
}

func (in CustomInput) fields() []EnvField {
	out := make([]EnvField, 0, len(in.Keys))
	for _, k := range in.Keys {
		out = append(out, EnvField{Name: k.Name, Label: k.Label, Hint: k.Hint, Secret: !k.Plain, Optional: k.Optional})
	}
	return out
}

// values reads what the keys hold: the typed ones first, then the safe secrets the keys name. missing lists the keys
// with no value (a required one with neither).
func (s *Service) customValues(in CustomInput, typed map[string]string) (vals map[string]string, missing []string) {
	vals = map[string]string{}
	for _, k := range in.Keys {
		v := strings.TrimSpace(typed[k.Name])
		if v == "" && k.Safe != "" {
			if got, err := s.secrets.Reveal(k.Safe); err == nil {
				v = strings.TrimSpace(got)
			}
		}
		if v == "" {
			if !k.Optional {
				missing = append(missing, k.Name)
			}
			continue
		}
		vals[k.Name] = v
	}
	return vals, missing
}

// Plan checks a service of one's own and says what a yes would make. It reaches the service (a probe of an MCP
// address; an API's check path when its keys are at hand) but never starts a local server and keeps nothing.
func (s *Service) PlanCustom(ctx context.Context, in CustomInput, typed map[string]string) (CustomPlan, error) {
	in, err := in.normalize()
	if err != nil {
		return CustomPlan{}, err
	}
	plan := CustomPlan{In: in}
	if old, ok := s.Custom(in.ID); ok {
		if kindOf(old) != in.Kind {
			return CustomPlan{}, fmt.Errorf("%w: %s is %s; remove it and add it again to make it %s", ErrBadRequest, in.ID, kindOf(old), in.Kind)
		}
		plan.Existing = &old
	} else if taken := s.slugTaken(in.ID); taken {
		return CustomPlan{}, fmt.Errorf("%w: %s exists but is not a service of one's own", ErrBadRequest, in.ID)
	}
	vals, missing := s.customValues(in, typed)
	switch in.Kind {
	case CustomMCP:
		p, err := s.probe(ctx, in.URL, in.Transport)
		if err != nil {
			return CustomPlan{}, fmt.Errorf("%w: %s does not answer as an MCP server (initialize failed)", ErrUnreachable, in.URL)
		}
		plan.In.URL, plan.In.Transport = p.URL, p.Transport
		auth, hdr := p.Auth, p.HeaderName
		if in.HeaderName != "" {
			hdr = in.HeaderName
		}
		if len(in.Keys) > 0 && auth != AuthOAuth {
			auth = AuthHeader
		}
		if hdr == "" {
			hdr = "Authorization"
		}
		plan.In.HeaderName = hdr
		switch {
		case auth == AuthNone:
			tools, err := s.inspect(ctx, Server{Transport: p.Transport, URL: p.URL}, nil)
			if err != nil {
				plan.Checked = locale.Pick("answers initialize, but tools/list failed", "отвечает на initialize, но tools/list не прошёл")
			} else {
				plan.Checked = locale.Pick(fmt.Sprintf("initialize and tools/list passed: %d tools, no sign-in", len(tools)),
					fmt.Sprintf("initialize и tools/list прошли: %d инструментов, вход не нужен", len(tools)))
			}
		case auth == AuthOAuth:
			plan.Checked = locale.Pick("the server signs in with OAuth: the person signs in on the service's page after the yes",
				"сервер входит по OAuth: человек войдёт на странице сервиса после «да»")
		case len(missing) == 0 && len(in.Keys) == 1:
			name, value, herr := headerOf(hdr, vals[in.Keys[0].Name])
			if herr != nil {
				return CustomPlan{}, herr
			}
			tools, err := s.inspect(ctx, Server{Transport: p.Transport, URL: p.URL}, http.Header{name: {value}})
			switch {
			case errors.Is(err, ErrDenied):
				return CustomPlan{}, fmt.Errorf("%w: the server refused the key from the safe", ErrDenied)
			case err != nil:
				plan.Checked = locale.Pick("the key was given, but tools/list failed", "ключ есть, но tools/list не прошёл")
			default:
				plan.Checked = locale.Pick(fmt.Sprintf("initialize and tools/list passed with the key: %d tools", len(tools)),
					fmt.Sprintf("initialize и tools/list прошли с ключом: %d инструментов", len(tools)))
			}
		default:
			plan.Checked = locale.Pick("the server needs a key in the header "+hdr+"; not checked until it is in the safe",
				"серверу нужен ключ в заголовке "+hdr+"; проверка после того, как он будет в сейфе")
		}
	case CustomAPI:
		srv := Server{URL: in.URL, APIHeaders: in.APIHeaders, APIQuery: in.APIQuery, CheckPath: in.CheckPath, Transport: API}
		switch {
		case len(missing) > 0:
			plan.Checked = locale.Pick("keys "+strings.Join(missing, ", ")+" are not in the safe yet: the check path is asked after they are",
				"ключей "+strings.Join(missing, ", ")+" в сейфе ещё нет: путь проверки спросим, когда они появятся")
		case in.CheckPath == "":
			plan.Checked = locale.Pick("no check path: the keys are not tried", "пути проверки нет: ключи не проверяются")
		default:
			if err := s.checkAPI(ctx, srv, vals); err != nil {
				if errors.Is(err, ErrDenied) {
					return CustomPlan{}, fmt.Errorf("%w: GET %s refused the keys from the safe", ErrDenied, in.CheckPath)
				}
				return CustomPlan{}, fmt.Errorf("%w: GET %s did not answer 2xx (%s)", ErrUnreachable, in.CheckPath, problemOf(err))
			}
			plan.Checked = locale.Pick("GET "+in.CheckPath+" answered 2xx with the keys", "GET "+in.CheckPath+" ответил 2xx с ключами")
		}
	case CustomLocal:
		plan.Checked = locale.Pick("not started: a local server runs only after the person's yes", "не запускался: локальный сервер стартует только после «да» человека")
	}
	plan.Pairs = plan.pairs()
	return plan, nil
}

func kindOf(srv Server) string {
	switch srv.Transport {
	case Stdio:
		return CustomLocal
	case API:
		return CustomAPI
	}
	return CustomMCP
}

func (s *Service) slugTaken(id string) bool {
	list, err := s.List()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(list, func(x Server) bool { return x.CatalogID == id })
}

// pairs are the lines of the card.
func (p CustomPlan) pairs() []Pair {
	in := p.In
	out := []Pair{{locale.Pick("Service", "Сервис"), in.Name + " (" + in.ID + ")"}}
	if p.Existing != nil {
		out = append(out, Pair{locale.Pick("Changes", "Правка"), locale.Pick("an existing service is updated", "обновляется уже заведённый сервис")})
	}
	switch in.Kind {
	case CustomMCP:
		out = append(out, Pair{locale.Pick("Kind", "Вид"), locale.Pick("MCP server by address", "MCP-сервер по адресу")},
			Pair{locale.Pick("Address", "Адрес"), in.URL})
	case CustomAPI:
		out = append(out, Pair{locale.Pick("Kind", "Вид"), locale.Pick("HTTP API", "HTTP API")}, Pair{locale.Pick("Base address", "Базовый адрес"), in.URL})
		if in.CheckPath != "" {
			out = append(out, Pair{locale.Pick("Check", "Проверка"), "GET " + in.CheckPath})
		}
		if len(in.APIHeaders) > 0 {
			var l []string
			for k, v := range in.APIHeaders {
				l = append(l, k+": "+v)
			}
			slices.Sort(l)
			out = append(out, Pair{locale.Pick("Keys go in headers", "Ключи уйдут в заголовки"), strings.Join(l, "; ")})
		}
		if len(in.APIQuery) > 0 {
			var l []string
			for k, v := range in.APIQuery {
				l = append(l, k+"="+v)
			}
			slices.Sort(l)
			out = append(out, Pair{locale.Pick("Keys go in the query", "Ключи уйдут в параметры"), strings.Join(l, "; ")})
		}
	case CustomLocal:
		out = append(out, Pair{locale.Pick("Kind", "Вид"), locale.Pick("local server", "локальный сервер")},
			Pair{locale.Pick("Runs on this machine", "Запустится на этой машине"), strings.Join(in.Command, " ")},
			Pair{locale.Pick("Attention", "Внимание"), locale.Pick(
				"this downloads and runs someone else's code with your keys in its environment; say yes only if you trust the package",
				"это скачает и запустит чужой код, ему отдадут ваши ключи в окружение; «да» только если вы доверяете пакету")})
	}
	for _, k := range in.Keys {
		line := k.Label
		if k.Plain {
			line += " (" + locale.Pick("a setting", "настройка") + ")"
		}
		if k.Safe != "" {
			line += " ← " + locale.Pick("safe: ", "сейф: ") + k.Safe
		} else {
			line += " ← " + locale.Pick("the person types it on the service's page", "человек введёт на странице сервиса")
		}
		out = append(out, Pair{k.Name, line})
	}
	if p.Checked != "" {
		out = append(out, Pair{locale.Pick("Tried", "Проверено"), p.Checked})
	}
	if in.About != "" {
		out = append(out, Pair{locale.Pick("About", "Описание"), clean(in.About, 300)})
	}
	if in.Guide != "" {
		out = append(out, Pair{locale.Pick("Guide for agents", "Памятка агентам"), clean(in.Guide, 300)})
	}
	return out
}

// SaveCustom keeps the service the plan describes: the card's yes. typed holds the keys the person typed, the safe
// secrets the keys name are copied in. A new MCP server with OAuth is kept waiting for the person's sign-in.
func (s *Service) SaveCustom(ctx context.Context, in CustomInput, typed map[string]string) (Server, *AuthStart, error) {
	plan, err := s.PlanCustom(ctx, in, typed)
	if err != nil {
		return Server{}, nil, err
	}
	in = plan.In
	vals, _ := s.customValues(in, typed)
	srv := Server{ID: newID(), Name: oneLine(in.Name), URL: in.URL, Transport: in.Transport, CatalogID: in.ID, Enabled: true,
		State: StateSignIn, CreatedAt: s.now().UTC(), Guide: in.Guide, CheckPath: in.CheckPath, APIHeaders: in.APIHeaders,
		APIQuery: in.APIQuery, Command: slices.Clone(in.Command), Env: in.fields(), HeaderName: in.HeaderName}
	meta := in.CustomMeta
	srv.Custom = &meta
	if old := plan.Existing; old != nil {
		srv.ID, srv.Slug, srv.CreatedAt, srv.Enabled = old.ID, old.Slug, old.CreatedAt, old.Enabled
	}
	var c cred
	if prev, err := s.readCred(srv.ID); err == nil {
		c = prev
	}
	switch in.Kind {
	case CustomMCP:
		p, err := s.probe(ctx, in.URL, in.Transport)
		if err != nil {
			return Server{}, nil, err
		}
		srv.URL, srv.Transport, srv.Auth, srv.OAuthUnavailable = p.URL, p.Transport, p.Auth, p.OAuthUnavailable
		if len(in.Keys) > 0 && srv.Auth != AuthOAuth {
			srv.Auth = AuthHeader
		}
		if srv.Auth == AuthHeader && srv.HeaderName == "" {
			srv.HeaderName = "Authorization"
		}
		srv.Env = nil
		if old := plan.Existing; old != nil && (old.URL != srv.URL || old.Auth != srv.Auth) {
			c = cred{} // another server or another sign-in: the old keys and tokens are not its
			s.dropFlows(old.ID)
		}
		var header http.Header
		if len(in.Keys) == 1 && vals[in.Keys[0].Name] != "" {
			name, value, herr := headerOf(srv.HeaderName, vals[in.Keys[0].Name])
			if herr != nil {
				return Server{}, nil, herr
			}
			c.Header, header = value, http.Header{name: {value}}
		} else if c.Header != "" && srv.Auth == AuthHeader {
			header = http.Header{srv.HeaderName: {c.Header}}
		}
		switch {
		case srv.Auth == AuthNone, header != nil:
			tools, err := s.inspect(ctx, srv, header)
			if errors.Is(err, ErrDenied) {
				return Server{}, nil, err
			}
			s.applyCheck(&srv, tools, err)
		default:
			// OAuth, or a header the person has not typed yet: it waits in "signin".
			if old := plan.Existing; old != nil && srv.Auth == old.Auth && c.OAuth != nil {
				srv.State, srv.Tools = old.State, old.Tools
			}
		}
	case CustomAPI:
		srv.Auth = AuthEnv
		if !slices.ContainsFunc(srv.Env, func(f EnvField) bool { return !f.Optional }) {
			srv.Auth = AuthNone
		}
		env := mergedEnv(c.Env, vals)
		c.Env = env
		if _, missing := envOfMissing(srv.Env, env); len(missing) > 0 {
			srv.State = StateSignIn
		} else {
			cerr := s.checkAPI(ctx, srv, env)
			if errors.Is(cerr, ErrDenied) {
				return Server{}, nil, cerr
			}
			s.applyCheck(&srv, apiTools(srv), cerr)
			srv.Problem = problemOf(cerr)
		}
		if srv.State == StateSignIn {
			srv.Tools = apiTools(srv)
		}
	case CustomLocal:
		srv.Auth = AuthEnv
		if !slices.ContainsFunc(srv.Env, func(f EnvField) bool { return !f.Optional }) {
			srv.Auth = AuthNone
		}
		srv.URL = "stdio:" + in.ID
		env := mergedEnv(c.Env, vals)
		c.Env = env
		runner.drop(srv.ID)
		if _, missing := envOfMissing(srv.Env, env); len(missing) > 0 {
			srv.State = StateSignIn
		} else {
			tools, cerr := s.inspectLocal(ctx, srv, env)
			s.applyCheck(&srv, tools, cerr)
			srv.Problem = problemOf(cerr)
		}
	}
	if c.Header != "" || c.OAuth != nil || len(c.Env) > 0 {
		if err := s.writeCred(srv.ID, c); err != nil {
			return Server{}, nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return Server{}, nil, err
	}
	if i := slices.IndexFunc(f.Servers, func(x Server) bool { return x.ID == srv.ID }); i >= 0 {
		srv.Slug = f.Servers[i].Slug
		f.Servers[i] = srv
	} else {
		srv.Slug = uniqueSlug(slugOf(strings.TrimPrefix(in.ID, CustomPrefix)), f.Servers, srv.ID)
		f.Servers = append(f.Servers, srv)
	}
	if err := s.writeAll(f); err != nil {
		return Server{}, nil, err
	}
	return srv, authStart(srv), nil
}

// mergedEnv is the old keys with the new ones over them.
func mergedEnv(old, add map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range old {
		out[k] = v
	}
	for k, v := range add {
		out[k] = v
	}
	return out
}

// envOfMissing is the required fields with no value.
func envOfMissing(fields []EnvField, env map[string]string) (map[string]string, []string) {
	var missing []string
	for _, f := range fields {
		if !f.Optional && env[f.Name] == "" && f.Default == "" {
			missing = append(missing, f.Name)
		}
	}
	return env, missing
}

// DescribeCustom changes how a service of one's own is described (the page's editor): name, group, mark, about, what
// it can do, steps, links, and an API's guide. The connection itself (address, keys) stays as it is.
func (s *Service) DescribeCustom(id string, name *string, guide *string, meta CustomMeta) (Server, error) {
	srv, err := s.Get(id)
	if err != nil {
		return Server{}, err
	}
	if srv.Custom == nil {
		return Server{}, fmt.Errorf("%w: only a service of one's own is described by hand", ErrBadRequest)
	}
	n := srv.Name
	if name != nil {
		n = *name
	}
	in := CustomInput{Kind: kindOf(srv), Name: n, ID: srv.CatalogID, URL: "https://x.invalid", Command: []string{"npx", "-y", "x@1.0.0"}, CustomMeta: meta}
	norm, err := in.normalize()
	if err != nil {
		return Server{}, err
	}
	return s.update(id, func(x *Server) error {
		m := norm.CustomMeta
		x.Custom = &m
		if name != nil {
			x.Name = norm.Name
		}
		if guide != nil && x.Transport == API {
			x.Guide = block(*guide, 4000)
		}
		return nil
	})
}

// ownIdentity makes srv a service of the person's own: the id custom-<slug> of its name, an empty description in the
// group "utility". The id is made unique when the server is kept (uniqueCustomID).
func (s *Service) ownIdentity(srv *Server) {
	id := CustomPrefix + slugOf(srv.Name)
	if len(id) > 40 {
		id = strings.TrimRight(id[:40], "-")
	}
	srv.CatalogID = id
	srv.Custom = &CustomMeta{Category: KindUtility, Letter: strings.ToUpper(string([]rune(srv.Name + "?")[:1]))}
}

// uniqueCustomID is id, or id-2, id-3, … when another server has it (and the catalog does not).
func uniqueCustomID(id string, list []Server, self string) string {
	taken := func(c string) bool {
		return KnownCatalogID(c) || slices.ContainsFunc(list, func(x Server) bool { return x.ID != self && x.CatalogID == c })
	}
	out := id
	for n := 2; taken(out); n++ {
		out = fmt.Sprintf("%s-%d", id, n)
	}
	return out
}

// adoptOwn gives the servers pasted by address before HRN-1707 (no catalog id, not an app's) the id and description of
// a service of one's own, so that an app's manifest can name them. It runs once when the service opens its file.
func (s *Service) adoptOwn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readAll()
	if err != nil {
		return
	}
	changed := false
	for i := range f.Servers {
		srv := &f.Servers[i]
		if srv.CatalogID != "" || srv.Owner != "" || srv.Custom != nil || srv.Transport == Stdio || srv.Transport == API {
			continue
		}
		s.ownIdentity(srv)
		srv.CatalogID = uniqueCustomID(srv.CatalogID, f.Servers, srv.ID)
		changed = true
	}
	if changed {
		_ = s.writeAll(f)
	}
}

// AttachSafeKeys gives an existing service of one's own the keys that sit in the safe under names it owns (keys[].safe,
// each starting with the service's id): the second step of a connection made before its key was there. The values are
// copied by the runtime; the service is tried with them and a refusal changes nothing.
func (s *Service) AttachSafeKeys(ctx context.Context, id string, keys []CustomKey) (Server, error) {
	srv, ok := s.Custom(id)
	if !ok {
		return Server{}, fmt.Errorf("%w: no service of your own %s: make it first", ErrNotFound, id)
	}
	vals := map[string]string{}
	for _, k := range keys {
		if k.Safe == "" || !strings.HasPrefix(k.Safe, id+"-") {
			return Server{}, fmt.Errorf("%w: the safe secret of %s must be named %s-<something>", ErrBadRequest, k.Name, id)
		}
		name := strings.ToUpper(strings.TrimSpace(k.Name))
		if kindOf(srv) != CustomMCP && !slices.ContainsFunc(srv.Env, func(f EnvField) bool { return f.Name == name }) {
			return Server{}, fmt.Errorf("%w: %s has no key %s", ErrBadRequest, id, name)
		}
		v, err := s.secrets.Reveal(k.Safe)
		if err != nil || strings.TrimSpace(v) == "" {
			return Server{}, fmt.Errorf("%w: %s is not in the safe yet: ask for it with `harnessly secret ask %s` and wait for \"ready\"", ErrBadRequest, k.Safe, k.Safe)
		}
		vals[name] = strings.TrimSpace(v)
	}
	if kindOf(srv) == CustomMCP {
		if len(vals) != 1 {
			return Server{}, fmt.Errorf("%w: an MCP server takes one key", ErrBadRequest)
		}
		for _, v := range vals {
			return s.SetHeader(ctx, srv.ID, srv.HeaderName, v)
		}
	}
	return s.SetEnv(ctx, srv.ID, vals)
}
