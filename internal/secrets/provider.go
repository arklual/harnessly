package secrets

import (
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// More reasons, of a safe whose values live on a server.
const (
	// ReasonDenied: the server refused this machine's login or this path.
	ReasonDenied = "denied"
	// ReasonUnavailable: the server did not answer, or answered that it cannot now.
	ReasonUnavailable = "unavailable"
	// ReasonReadOnly: a reference is read only; it is changed where it lives.
	ReasonReadOnly = "read-only"
)

// Where a secret lives, as View.Where says it when the safe has more than one place.
const (
	WhereLocal = "local"
	WhereVault = "vault"
)

// Provider is a place the safe keeps secrets in: this machine's sealed file (Store) or a Vault-compatible server
// (Remote). Every method refuses with a Refusal; none puts a value into an error.
type Provider interface {
	List() ([]View, error)
	Has(name string) bool
	Put(name, value string) (View, bool, error)
	Remove(name string) error
	Reveal(names []string, use Use) (map[string]string, error)
	// Lookup, Metadata, Grant, GrantOrigin and PutAsked keep the limits of a secret: the projects whose chats may use
	// it and the sites it may be typed into (access.go). They hold no value.
	Lookup(name, project string) (View, error)
	Metadata(name string, origins, projects []string) (View, error)
	Grant(name, project, origin string) error
	GrantOrigin(name, project, origin string) error
	PutAsked(a Ask, value string) (View, bool, error)
}

// RefReader reads a secret an administrator keeps on the server, by its path and key: a reference.
type RefReader interface {
	ReadRef(path, key string) (string, error)
}

var (
	_ Provider = (*Store)(nil)
	_ Provider = (*Safe)(nil)
)

var (
	refPathRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	refKeyRe  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

// Ref is a name a command may use besides a plain secret name:
//
//	local:<name>          the secret <name> of this machine's file, even when the safe keeps new secrets elsewhere
//	vault:<path>#<key>    the key <key> at <path> of the server's mount, read only (no #<key>: the key "value")
type Ref struct {
	// Kind is "" for a plain name, WhereLocal or WhereVault.
	Kind string
	Name string
	Path string
	Key  string
}

// ParseRef reads a name or a reference; ok is false for anything else.
func ParseRef(s string) (Ref, bool) {
	if ValidName(s) {
		return Ref{Name: s}, true
	}
	if rest, ok := strings.CutPrefix(s, "local:"); ok {
		return Ref{Kind: WhereLocal, Name: rest}, ValidName(rest)
	}
	rest, ok := strings.CutPrefix(s, "vault:")
	if !ok || len(rest) > 640 {
		return Ref{}, false
	}
	path, key, hasKey := strings.Cut(rest, "#")
	if !hasKey {
		key = "value"
	}
	if !refPathRe.MatchString(path) || !refKeyRe.MatchString(key) {
		return Ref{}, false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "." || seg == ".." {
			return Ref{}, false
		}
	}
	return Ref{Kind: WhereVault, Path: path, Key: key}, true
}

// ValidRef says whether s is a secret's name or a reference a command may use.
func ValidRef(s string) bool {
	_, ok := ParseRef(s)
	return ok
}

// FileName is a file name for the value of s (a valid name or reference): the name itself, or for a vault: reference
// the last segment of its path and its key, "deploy-token" for vault:shared/ci/deploy#token.
func FileName(s string) string {
	r, ok := ParseRef(s)
	switch {
	case !ok:
		return "secret"
	case r.Kind != WhereVault:
		return r.Name
	}
	base := r.Path[strings.LastIndex(r.Path, "/")+1:]
	if r.Key != "value" {
		base += "-" + r.Key
	}
	if base == "." || base == ".." || strings.HasPrefix(base, ".") {
		base = "secret" + base
	}
	return base
}

// Safe is what the routes use: the machine's file, and, when the safe keeps its secrets on a server, that server
// (Primary) and its references. A plain name is looked up in Primary first, then in the file, so the secrets put before
// the switch keep working until they are moved. New secrets go to Primary.
type Safe struct {
	Local *Store
	// Primary is where new secrets go; nil keeps everything in Local.
	Primary Provider
	// Refs reads vault: references; nil refuses them.
	Refs RefReader
	// Where names Primary for View.Where (WhereVault).
	Where string
}

func (s *Safe) primary() Provider {
	if s.Primary == nil {
		return s.Local
	}
	return s.Primary
}

func (s *Safe) split() bool { return s.Primary != nil }

// List is the secrets of Primary and then those of the file it does not shadow, each with where it lives. With the
// file alone, the list is the file's, without where.
func (s *Safe) List() ([]View, error) {
	if !s.split() {
		return s.Local.List()
	}
	remote, err := s.Primary.List()
	if err != nil {
		return nil, err
	}
	local, err := s.Local.List()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(remote))
	out := make([]View, 0, len(remote)+len(local))
	for _, v := range remote {
		v.Where = s.Where
		seen[v.Name] = true
		out = append(out, v)
	}
	for _, v := range local {
		if seen[v.Name] {
			continue
		}
		v.Where = WhereLocal
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Has says whether a name can be used now; a vault: reference is not looked up (it is read when it is used).
func (s *Safe) Has(name string) bool {
	ref, ok := ParseRef(name)
	switch {
	case !ok:
		return false
	case ref.Kind == WhereLocal:
		return s.Local.Has(ref.Name)
	case ref.Kind == WhereVault:
		return false
	case s.split() && s.Primary.Has(ref.Name):
		return true
	default:
		return s.Local.Has(ref.Name)
	}
}

// Put keeps a value in Primary. Only plain names are put: a reference is changed where it lives. A secret of the file
// given a new value moves to Primary: its old copy leaves the file, so no stale value stays behind it.
func (s *Safe) Put(name, value string) (View, bool, error) {
	if !ValidName(name) {
		if ValidRef(name) {
			return View{}, false, refuse(ReasonReadOnly, nil)
		}
		return View{}, false, refuse(ReasonBadName, nil)
	}
	var moved *View
	if s.split() && !s.Primary.Has(name) && s.Local.Has(name) {
		if views, err := s.Local.List(); err == nil {
			for i := range views {
				if views[i].Name == name {
					moved = &views[i]
				}
			}
		}
	}
	v, created, err := s.primary().Put(name, value)
	if err == nil && s.split() {
		if moved != nil {
			// The limits of the file's copy go with its value to the server.
			if mv, merr := s.Primary.Metadata(name, moved.Origins, moved.Projects); merr == nil {
				v = mv
			}
		}
		v.Where = s.Where
		if s.Local.Has(name) {
			// The value is on the server already; a copy the file cannot forget now is only shadowed, and the
			// next Put moves it again.
			_ = s.Local.Remove(name)
		}
	}
	return v, created, err
}

// Remove forgets a secret in Primary, or, when Primary has none of that name, in the file (local:<name> says the
// file outright).
func (s *Safe) Remove(name string) error {
	ref, ok := ParseRef(name)
	switch {
	case !ok:
		return refuse(ReasonBadName, nil)
	case ref.Kind == WhereVault:
		return refuse(ReasonReadOnly, nil)
	case ref.Kind == WhereLocal || !s.split():
		return s.Local.Remove(ref.Name)
	}
	err := s.Primary.Remove(ref.Name)
	if ReasonOf(err) == ReasonNotFound {
		return s.Local.Remove(ref.Name)
	}
	return err
}

// holder is where a plain name lives now: Primary when it keeps the name, otherwise the file.
func (s *Safe) holder(name string) Provider {
	if s.split() && s.Primary.Has(name) {
		return s.Primary
	}
	return s.Local
}

// Lookup is what a project's chat may know of a secret: its public view, or not-found for a foreign one.
func (s *Safe) Lookup(name, project string) (View, error) {
	v, err := s.holder(name).Lookup(name, project)
	if err == nil && s.split() {
		v.Where = s.whereOf(name)
	}
	return v, err
}

func (s *Safe) whereOf(name string) string {
	if s.Primary.Has(name) {
		return s.Where
	}
	return WhereLocal
}

// Metadata changes the limits of a secret where it lives.
func (s *Safe) Metadata(name string, origins, projects []string) (View, error) {
	return s.holder(name).Metadata(name, origins, projects)
}

// Grant lets a project (and a site) use a secret, after a person answered its card.
func (s *Safe) Grant(name, project, origin string) error {
	return s.holder(name).Grant(name, project, origin)
}

// GrantOrigin lets a secret be typed into one more site.
func (s *Safe) GrantOrigin(name, project, origin string) error {
	return s.holder(name).GrantOrigin(name, project, origin)
}

// PutAsked keeps a value a card asked for. With a server, it goes there, as Put; a copy in the file that belongs to
// another project is not overwritten, and the limits of one this project may use go with its value.
func (s *Safe) PutAsked(a Ask, value string) (View, bool, error) {
	if !s.split() {
		return s.Local.PutAsked(a, value)
	}
	var moved *View
	if !s.Primary.Has(a.Name) && s.Local.Has(a.Name) {
		lv, err := s.Local.Lookup(a.Name, a.Project)
		if err != nil {
			return View{}, false, err
		}
		moved = &lv
	}
	v, created, err := s.Primary.PutAsked(a, value)
	if err != nil {
		return v, created, err
	}
	if moved != nil {
		if mv, merr := s.Primary.Metadata(a.Name, union(moved.Origins, v.Origins), moved.Projects); merr == nil {
			v = mv
		}
		_ = s.Local.Remove(a.Name)
	}
	v.Where = s.Where
	return v, created, nil
}

func union(a, b []string) []string {
	out := append([]string{}, a...)
	for _, x := range b {
		out = appendOnce(out, x)
	}
	return out
}

// Reveal opens every name for one command: plain names from Primary, then the file; local: from the file; vault:
// references from the server. Nothing is returned when one of them cannot be opened. The values are keyed by the
// names as asked.
func (s *Safe) Reveal(names []string, use Use) (map[string]string, error) {
	out := make(map[string]string, len(names))
	var local, plain []string
	for _, name := range names {
		ref, ok := ParseRef(name)
		if !ok {
			return nil, refuse(ReasonBadName, errors.New(name))
		}
		switch {
		case ref.Kind == WhereLocal:
			local = appendOnce(local, ref.Name)
		case ref.Kind == WhereVault:
			if s.Refs == nil {
				return nil, refuse(ReasonNotFound, errors.New(name))
			}
			v, err := s.Refs.ReadRef(ref.Path, ref.Key)
			if err != nil {
				return nil, err
			}
			out[name] = v
		case s.split():
			plain = append(plain, ref.Name)
		default:
			local = appendOnce(local, ref.Name)
		}
	}
	for _, name := range plain {
		values, err := s.Primary.Reveal([]string{name}, use)
		if ReasonOf(err) == ReasonNotFound && s.Local.Has(name) {
			local = appendOnce(local, name)
			continue
		}
		if err != nil {
			return nil, err
		}
		out[name] = values[name]
	}
	if len(local) > 0 {
		values, err := s.Local.Reveal(local, use)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if n := strings.TrimPrefix(name, "local:"); values[n] != "" {
				if _, done := out[name]; !done {
					out[name] = values[n]
				}
			}
		}
	}
	return out, nil
}

func appendOnce(list []string, name string) []string {
	if slices.Contains(list, name) {
		return list
	}
	return append(list, name)
}
