package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Opening a document of an app's type. The document's data is the value under SinopiaKind.DataKey in the app's hl.storage.doc
// area of that document; the host keeps the version of the format beside it, under SchemaKey. A document whose recorded
// version is older than the app's is migrated when it is opened: the old value is kept under <dataKey>@v<N> in the same
// write as the new one, so the migration is all or nothing and RevertMigration brings the old document back.

// SchemaKey is the key of the area that holds the format version of the document, written by the host.
const SchemaKey = "@schema"

func backupKey(dataKey string, version int) string { return dataKey + "@v" + strconv.Itoa(version) }

// KindOpen is what opening a document of an app's type found.
type KindOpen struct {
	// Version is the document's format version after opening (0 when none is recorded); Current is the app's.
	Version int `json:"version"`
	Current int `json:"current"`
	// Migrated is set when this opening migrated the document, from From.
	Migrated bool `json:"migrated"`
	From     int  `json:"from,omitempty"`
	// ReadOnly is set when the document cannot be opened for editing, with Reason.
	ReadOnly bool `json:"readOnly"`
	// Code says why in a word the page turns into plain words of its own language; Reason is the detail.
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// The codes of a read-only opening.
const (
	ReadOnlyNewer      = "newer"      // the document is of a newer version than the app knows
	ReadOnlyFailed     = "failed"     // a migration step does not fit the document
	ReadOnlyNoRoom     = "no-room"    // no room to keep the old copy next to the new one
	ReadOnlyUnreadable = "unreadable" // the version or the document cannot be read
	ReadOnlyNoApp      = "no-package" // the app's package cannot be read, or names no key for the document
	ReadOnlyViewOnly   = "view-only"  // the person may only view the project (its role is below edit)
)

// RecordDocVersion notes the format version a new document is made at.
func RecordDocVersion(st *Storage, app, project string, version int) error {
	return st.SetMany(app, ScopeDoc, project, map[string]json.RawMessage{SchemaKey: json.RawMessage(strconv.Itoa(version))})
}

// OpenDocument migrates a document of an older version, or says why it is read-only. It never leaves a document half
// migrated: the value and the version change in one write, or neither does.
func (r *Registry) OpenDocument(st *Storage, project string, info KindInfo, k SinopiaKind) KindOpen {
	out := KindOpen{Current: k.SchemaVersion}
	readOnly := func(code, format string, a ...any) KindOpen {
		out.ReadOnly, out.Code, out.Reason = true, code, fmt.Sprintf(format, a...)
		return out
	}
	raw, ok, err := st.Get(info.App, ScopeDoc, project, SchemaKey)
	if err != nil {
		return readOnly(ReadOnlyUnreadable, "the version of the document cannot be read: %v", err)
	}
	if !ok {
		// Nothing is recorded: there is nothing the host knows to migrate from.
		return out
	}
	from, err := strconv.Atoi(string(raw))
	if err != nil || from < 1 {
		return readOnly(ReadOnlyUnreadable, "the recorded version of the document is not a version")
	}
	out.Version = from
	switch {
	case from == k.SchemaVersion:
		return out
	case from > k.SchemaVersion:
		return readOnly(ReadOnlyNewer, "the document is of version %d, the installed app knows version %d", from, k.SchemaVersion)
	}
	if k.DataKey == "" {
		return readOnly(ReadOnlyNoApp, "the app names no key that holds the document, so it cannot be migrated")
	}
	list, err := r.List()
	if err != nil {
		return readOnly(ReadOnlyUnreadable, "%v", err)
	}
	var pkg *Package
	for _, a := range list {
		if a.ID == info.App {
			pkg, _ = CachedPackage(a.Dir)
		}
	}
	if pkg == nil {
		return readOnly(ReadOnlyNoApp, "the package of %s cannot be read", info.App)
	}
	doc, ok, err := st.Get(info.App, ScopeDoc, project, k.DataKey)
	if err != nil {
		return readOnly(ReadOnlyUnreadable, "the document cannot be read: %v", err)
	}
	if !ok {
		// A document with no data yet is only brought to the current version.
		if err := RecordDocVersion(st, info.App, project, k.SchemaVersion); err != nil {
			return readOnly(ReadOnlyUnreadable, "%v", err)
		}
		out.Version = k.SchemaVersion
		return out
	}
	migrated, err := k.Migrate(pkg, doc, from)
	if err != nil {
		return readOnly(ReadOnlyFailed, "the document is of version %d and could not be brought to %d: %v", from, k.SchemaVersion, err)
	}
	err = st.SetMany(info.App, ScopeDoc, project, map[string]json.RawMessage{
		k.DataKey:                  migrated,
		backupKey(k.DataKey, from): doc,
		SchemaKey:                  json.RawMessage(strconv.Itoa(k.SchemaVersion)),
	})
	if errors.Is(err, ErrQuota) {
		return readOnly(ReadOnlyNoRoom, "there is no room to keep the old copy of the document next to the new one")
	}
	if err != nil {
		return readOnly(ReadOnlyUnreadable, "%v", err)
	}
	out.Version, out.Migrated, out.From = k.SchemaVersion, true, from
	return out
}

// RevertMigration brings a migrated document back to the version it was before, from the copy the migration kept.
func (r *Registry) RevertMigration(st *Storage, project string, info KindInfo, k SinopiaKind, to int) error {
	if k.DataKey == "" || to < 1 || to >= k.SchemaVersion {
		return fmt.Errorf("%w: nothing to go back to", ErrMigration)
	}
	old, ok, err := st.Get(info.App, ScopeDoc, project, backupKey(k.DataKey, to))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: no copy of version %d was kept", ErrMigration, to)
	}
	if err := st.SetMany(info.App, ScopeDoc, project, map[string]json.RawMessage{k.DataKey: old, SchemaKey: json.RawMessage(strconv.Itoa(to))}); err != nil {
		return err
	}
	return st.Delete(info.App, ScopeDoc, project, backupKey(k.DataKey, to))
}
