// Package appstore is the storage of an app's views (hl.storage.<scope>) without the rest of the apps: the areas, their
// limits, the versions of their keys (ifVersion) and where an area's file lives. The runtime keeps the install's area
// on its disk and, under a hub, every other area in the hub's store (HRN-898), so the data of an app is the same on
// every machine of a space and is there with the machines off; the hub answers a view it serves with the same code.
// It imports nothing of the runtime or the hub.
package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Scope is an area of the storage.
type Scope string

const (
	ScopeInstall Scope = "install"
	ScopeUser    Scope = "user"
	ScopeChat    Scope = "chat"
	ScopeSpace   Scope = "space"
	ScopeDoc     Scope = "doc"
)

// Limit is the most one area may hold, in bytes of its JSON file.
var Limit = map[Scope]int{
	ScopeInstall: 5 << 20,
	ScopeUser:    1 << 20,
	ScopeChat:    256 << 10,
	ScopeSpace:   10 << 20,
	ScopeDoc:     100 << 10,
}

// Total is the most all the areas of one scope of one app may hold together, so many chats or documents cannot add
// up past what the scope is meant to cost.
var Total = map[Scope]int64{
	ScopeInstall: 5 << 20,
	ScopeUser:    1 << 20,
	ScopeChat:    20 << 20,
	ScopeSpace:   10 << 20,
	ScopeDoc:     20 << 20,
}

// PerOwner are the scopes whose area belongs to a chat or a document.
func (s Scope) PerOwner() bool { return s == ScopeChat || s == ScopeDoc }

// Known tells a scope from a text that is not one.
func (s Scope) Known() bool { _, ok := Limit[s]; return ok }

// MaxAreaBytes is the largest area of any scope.
const MaxAreaBytes = 10 << 20

// MaxKeyBytes is the longest key.
const MaxKeyBytes = 256

var (
	// ErrQuota: the write would take the area past its limit.
	ErrQuota = errors.New("apps: the storage area is full")
	// ErrArgs: a scope, owner or key that is not one.
	ErrArgs = errors.New("apps: bad storage arguments")
	// ErrConflict: a write that named the version it read (ifVersion) found the key moved on. The error is a
	// *ConflictError, which carries the version the key has now.
	ErrConflict = errors.New("apps: the key changed since it was read")
	// ErrMoved: an area's file is no longer at the revision a write was made on (another machine wrote it); the
	// write is made again on top of the new one.
	ErrMoved = errors.New("apps: the area changed under the write")

	ownerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	// nameRe is an area's file name: an owner, or under a hub a person's name before a chat's ("<person>.<chat>").
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
	appRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39}$`)
)

// ConflictError is the refusal of a write whose ifVersion is not the key's version; Current is the version it has.
type ConflictError struct{ Current int64 }

func (e *ConflictError) Error() string        { return ErrConflict.Error() }
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// ValidOwner says whether a text can name the chat or document an area belongs to.
func ValidOwner(owner string) bool { return ownerRe.MatchString(owner) }

// ValidApp says whether a text is an app's id.
func ValidApp(app string) bool { return appRe.MatchString(app) }

// VersionsKey is where an area's file keeps the version of each key, next to the values. A key's version is a counter
// that only grows: a write adds one, a delete adds one too and keeps the number, so a key made anew never repeats a
// version a reader holds. A key with no version (data written before versions) is at 0. The name is not a key a
// caller can use.
const VersionsKey = "__hl_versions"

// CheckKey refuses a key a caller may not use.
func CheckKey(key string) error {
	if key == "" || len(key) > MaxKeyBytes || key == VersionsKey {
		return ErrArgs
	}
	return nil
}

// Path is where an area's file is, relative to the storage's root: <app>/<scope>/<name>.json. name is the chat or
// document for a scope per owner, and "area" for the others; in the hub's store a user area is named by the person
// and a chat's by the person and the chat (<person>.<chat>), so the hub shows them only to that person's machines.
func Path(app string, scope Scope, name string) (string, error) {
	if !ValidApp(app) || !scope.Known() || !nameRe.MatchString(name) {
		return "", ErrArgs
	}
	return app + "/" + string(scope) + "/" + name + ".json", nil
}

// Area is an area as its file holds it: the values and the version of each key.
type Area struct {
	Values   map[string]json.RawMessage
	Versions map[string]int64
}

// Parse reads an area's file; nil or empty is an empty area.
func Parse(raw []byte) (*Area, error) {
	a := &Area{Values: map[string]json.RawMessage{}, Versions: map[string]int64{}}
	if len(raw) == 0 {
		return a, nil
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	for k, v := range all {
		if k == VersionsKey {
			// A damaged versions record is the same as none: every key is back at 0.
			_ = json.Unmarshal(v, &a.Versions)
			if a.Versions == nil {
				a.Versions = map[string]int64{}
			}
			continue
		}
		a.Values[k] = v
	}
	return a, nil
}

// Marshal is the area's file.
func (a *Area) Marshal() ([]byte, error) {
	out := make(map[string]json.RawMessage, len(a.Values)+1)
	for k, v := range a.Values {
		out[k] = v
	}
	if len(a.Versions) > 0 {
		raw, err := json.Marshal(a.Versions)
		if err != nil {
			return nil, err
		}
		out[VersionsKey] = raw
	}
	return json.Marshal(out)
}

// Keys are the area's keys, sorted.
func (a *Area) Keys() []string {
	keys := make([]string, 0, len(a.Values))
	for k := range a.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Files is where areas' files are kept: a folder on the machine, or a room of the hub's store.
type Files interface {
	// Read is a file and the revision it is at; nil and the revision of its deletion (0 when it never was) when
	// there is none.
	Read(ctx context.Context, rel string) (data []byte, rev int64, err error)
	// Write puts a file (nil deletes it) on top of the revision base Read gave; ErrMoved when it is no longer there.
	Write(ctx context.Context, rel string, base int64, data []byte) error
	// Weigh is what the files under a folder (rel, no slash at the end) weigh together.
	Weigh(ctx context.Context, dir string) (int64, error)
}

// Read is an area as its file has it now.
func Read(ctx context.Context, f Files, rel string) (*Area, error) {
	raw, _, err := f.Read(ctx, rel)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// attempts is how many times a write is made again when another machine wrote the area in between.
const attempts = 8

// Update changes an area: change gets the area as it is and says whether it changed it (or refuses with an error); the
// file is written only then, and only inside the scope's limits (quota false: a delete, which never needs room). A
// write that finds the area moved on runs change again on the new one. It answers the area as written.
func Update(ctx context.Context, f Files, scope Scope, rel string, quota bool, change func(*Area) (bool, error)) (*Area, error) {
	dir, _, _ := strings.Cut(rel, "/"+lastPart(rel))
	for i := 0; i < attempts; i++ {
		raw, rev, err := f.Read(ctx, rel)
		if err != nil {
			return nil, err
		}
		a, err := Parse(raw)
		if err != nil {
			return nil, err
		}
		changed, err := change(a)
		if err != nil || !changed {
			return a, err
		}
		out, err := a.Marshal()
		if err != nil {
			return nil, err
		}
		if quota && len(out) > Limit[scope] {
			return nil, ErrQuota
		}
		// The scope's other areas (a scope per chat or document has many), and this one as it was, are kept: the
		// write adds the difference. Under a hub they are the space's, not one machine's.
		if quota && scope.PerOwner() {
			all, err := f.Weigh(ctx, dir)
			if err != nil {
				return nil, err
			}
			if all-int64(len(raw))+int64(len(out)) > Total[scope] {
				return nil, ErrQuota
			}
		}
		switch err := f.Write(ctx, rel, rev, out); {
		case errors.Is(err, ErrMoved):
			continue
		case err != nil:
			return nil, err
		}
		return a, nil
	}
	return nil, fmt.Errorf("%w: %s written by others %d times in a row", ErrMoved, rel, attempts)
}

func lastPart(rel string) string { return rel[strings.LastIndex(rel, "/")+1:] }
