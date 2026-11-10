// Package profilesync syncs a person's Claude Code profile through the hub's store: the room is
// the person's (filestore.Profile), sealed with a room key that the hub's safe keeps for the person, and the plugins the
// settings enable are installed on each machine. It takes over from design-sync's profile agents
// (design-sync-profile-*), whose rooms and key it keeps: a machine that still has a design-sync identity opens the key
// wrapped for it once and puts it in the safe, and from then on every machine of the person reads it from there.
package profilesync

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// KeySecret is the name the room key is kept under in the person's folder of the hub's safe.
const KeySecret = "profile-room-key"

// Safe is the person's folder of the hub's safe (agent.LocalSafe under "me/").
type Safe interface {
	Read(ctx context.Context, path string) (map[string]string, error)
	Write(ctx context.Context, path string, data map[string]string) error
}

// Room is what the key needs of the profile's room.
type Room interface {
	Keys(ctx context.Context) ([]filestore.WrappedKey, error)
	Changes(ctx context.Context, since int64, wait time.Duration) (filestore.Changes, error)
}

// ErrNoKey: the room has a key, and neither the safe nor any design-sync identity on this machine opens it. Another
// machine of the person that has one puts it in the safe as soon as it runs.
var ErrNoKey = errors.New("the profile's room key is neither in the hub's safe nor wrapped for this machine")

// Key finds the room key: the safe's; else the one design-sync wrapped for an identity on this machine (put in the
// safe then); else, for a room nobody keyed yet and that holds nothing, a new one (put in the safe). folder is the
// safe's folder of the person ("me").
func Key(ctx context.Context, safe Safe, folder string, room Room, identities []string) ([]byte, error) {
	path := folder + "/" + KeySecret
	data, err := safe.Read(ctx, path)
	switch {
	case err == nil:
		k, err := base64.StdEncoding.DecodeString(data[secrets.ValueKey])
		if err != nil || len(k) != filesync.RoomKeySize {
			return nil, fmt.Errorf("the hub's safe keeps %s, and it is no room key", KeySecret)
		}
		return k, nil
	case secrets.ReasonOf(err) != secrets.ReasonNotFound:
		return nil, err
	}
	wrapped, err := room.Keys(ctx)
	if err != nil {
		return nil, err
	}
	var key []byte
	for _, w := range wrapped {
		for _, file := range identities {
			if k, err := unwrapWith(w.Wrapped, file); err == nil {
				key = k
				break
			}
		}
		if key != nil {
			break
		}
	}
	if key == nil {
		if len(wrapped) > 0 {
			return nil, ErrNoKey
		}
		ch, err := room.Changes(ctx, 0, 0)
		if err != nil {
			return nil, err
		}
		if len(ch.Entries) > 0 {
			return nil, ErrNoKey
		}
		key = make([]byte, filesync.RoomKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
	}
	if err := safe.Write(ctx, path, map[string]string{secrets.ValueKey: base64.StdEncoding.EncodeToString(key)}); err != nil {
		return nil, err
	}
	// Two machines may have put one at once: the safe's is the room's.
	data, err = safe.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	k, err := base64.StdEncoding.DecodeString(data[secrets.ValueKey])
	if err != nil || len(k) != filesync.RoomKeySize {
		return nil, fmt.Errorf("the hub's safe keeps %s, and it is no room key", KeySecret)
	}
	return k, nil
}

// unwrapWith opens a room key design-sync wrapped (age, armored) with the identity in file.
func unwrapWith(wrapped, file string) ([]byte, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	id, err := age.ParseX25519Identity(strings.TrimSpace(string(b)))
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(armor.NewReader(strings.NewReader(wrapped)), id)
	if err != nil {
		return nil, err
	}
	k, err := io.ReadAll(io.LimitReader(r, 64))
	if err != nil {
		return nil, err
	}
	if len(k) != filesync.RoomKeySize {
		return nil, fmt.Errorf("room key has %d bytes", len(k))
	}
	return k, nil
}

// Identities are design-sync's identities on this machine: its profile agents kept them in the profile's state folder
// (state, absolute), as agent-<person>/identity.age.
func Identities(state string) []string {
	out, _ := filepath.Glob(filepath.Join(state, "agent-*", "identity.age"))
	return out
}
