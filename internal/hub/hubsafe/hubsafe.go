// Package hubsafe keeps the safes of a hub's machines on one Vault-compatible server, the hub's (docs/safe.md, "The
// hub's safe"). Only the hub logs in to the server; a machine reaches nothing but its owner's folder, and the hub
// builds that folder itself from the machine it verified:
//
//	<prefix>/users/<owner id>/<name>   a secret of the machine's owner, the value under "value"
//
// A machine may also read a key an administrator keeps at a path (a vault: reference), when the path lies under one of
// RefPrefixes and outside every person's folder.
package hubsafe

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/secrets/vaultkv"
)

// Broker answers the requests of machines to their safes.
type Broker struct {
	KV secrets.KV
	// Prefix is the hub's folder in the mount (harnessly).
	Prefix string
	// RefPrefixes are the folders references may read; none refuses them.
	RefPrefixes []string
}

// New is a broker over kv under prefix.
func New(kv secrets.KV, prefix string, refs []string) *Broker {
	return &Broker{KV: kv, Prefix: strings.Trim(prefix, "/"), RefPrefixes: refs}
}

func refuse(reason, why string) error {
	return &secrets.Refusal{Reason: reason, Err: errors.New(why)}
}

// Folder is the folder of a person's secrets.
func (b *Broker) Folder(ownerID string) string {
	return b.Prefix + "/users/" + ownerID
}

// usersRoot is the folder all persons' folders lie in: a reference never reads under it.
func (b *Broker) usersRoot() string { return b.Prefix + "/users" }

// Do runs one request for the machine whose owner is ownerID. names is the answer of a list, data the one of a read.
// Every error is a secrets.Refusal and never carries a value.
func (b *Broker) Do(ctx context.Context, ownerID string, req hubproto.SecretsRequest) (names []string, data map[string]string, err error) {
	// The owner's id comes from the hub's own records; it is checked all the same, as it becomes a path.
	if ownerID == "" || strings.Contains(ownerID, "/") || !vaultkv.ValidPath(ownerID) {
		return nil, nil, refuse(secrets.ReasonDenied, "the machine has no owner the safe can name")
	}
	folder := b.Folder(ownerID)
	switch req.Op {
	case hubproto.SecretsList:
		keys, err := b.KV.List(ctx, folder)
		if secrets.ReasonOf(err) == secrets.ReasonNotFound {
			return []string{}, nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		names = []string{}
		for _, k := range keys {
			if secrets.ValidName(k) {
				names = append(names, k)
			}
		}
		slices.Sort(names)
		return names, nil, nil
	case hubproto.SecretsRead, hubproto.SecretsWrite, hubproto.SecretsDelete:
		// A name is one segment of letters, digits, '.', '_' and '-': it cannot leave the owner's folder.
		if !secrets.ValidName(req.Name) {
			return nil, nil, refuse(secrets.ReasonBadName, "not a name of the safe")
		}
		path := folder + "/" + req.Name
		switch req.Op {
		case hubproto.SecretsRead:
			data, err := b.KV.Read(ctx, path)
			return nil, data, err
		case hubproto.SecretsWrite:
			v, ok := req.Data[secrets.ValueKey]
			if !ok || len(req.Data) != 1 {
				return nil, nil, refuse(secrets.ReasonEmpty, "write keeps one value")
			}
			if len(v) > secrets.MaxValue {
				return nil, nil, refuse(secrets.ReasonTooLarge, "the value is too large")
			}
			return nil, nil, b.KV.Write(ctx, path, map[string]string{secrets.ValueKey: v})
		default:
			return nil, nil, b.KV.Delete(ctx, path)
		}
	case hubproto.SecretsRef:
		path := strings.Trim(req.Path, "/")
		if !b.RefAllowed(path) {
			return nil, nil, refuse(secrets.ReasonDenied, "the path is outside the references this hub lets machines read")
		}
		data, err := b.KV.Read(ctx, path)
		return nil, data, err
	}
	return nil, nil, refuse(secrets.ReasonFailed, "unknown operation")
}

// RefAllowed says whether a machine may read path as a reference: a valid path under one of RefPrefixes and outside the
// persons' folders, whatever the prefixes say.
func (b *Broker) RefAllowed(path string) bool {
	if !vaultkv.ValidPath(path) || under(path, b.usersRoot()) {
		return false
	}
	for _, p := range b.RefPrefixes {
		if p == "*" || under(path, p) {
			return true
		}
	}
	return false
}

func under(path, folder string) bool {
	folder = strings.Trim(folder, "/")
	return folder != "" && (path == folder || strings.HasPrefix(path, folder+"/"))
}
