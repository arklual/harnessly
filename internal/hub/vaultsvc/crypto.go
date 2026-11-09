// Package vaultsvc is the hub's central password safe (docs/hub/vault.md): entries sealed with AES-256-GCM under a
// master key that lives only in the hub's environment, a per-person audit, grants for agent tabs, and one-time fill
// tokens. No route answers a password: the page gets a token, and the value goes out only through Redeem, which the
// preview side calls with what the slot's signed cookie says.
package vaultsvc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	stdbase64 "encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Keyring is the master keys by id; Current seals every new or rewritten row.
type Keyring struct {
	Current string
	Keys    map[string][]byte
}

type sealed struct {
	KeyID  string
	Nonce  []byte
	Secret []byte // ciphertext followed by the 16-byte GCM tag
}

var (
	keyID = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	b64Re = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
)

const (
	nonceBytes = 12
	tagBytes   = 16
)

// ParseKeys reads `v2:<base64 32 bytes>,v1:<base64>`, the first one current. Empty means no safe: there is no default
// key. Errors name the position, never the value.
func ParseKeys(spec string) (*Keyring, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, errors.New("HUB_VAULT_KEYS is not set")
	}
	kr := &Keyring{Keys: map[string][]byte{}}
	for i, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		id, enc, ok := strings.Cut(part, ":")
		if !ok || !keyID.MatchString(id) {
			return nil, fmt.Errorf("key %d: want <id>:<base64>, the id from a-z0-9-", i+1)
		}
		if _, dup := kr.Keys[id]; dup {
			return nil, fmt.Errorf("key %s is given twice", id)
		}
		if !b64Re.MatchString(enc) {
			return nil, fmt.Errorf("key %s: not base64", id)
		}
		raw, err := stdbase64.StdEncoding.DecodeString(enc)
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("key %s: exactly 32 bytes in base64 are needed", id)
		}
		kr.Keys[id] = raw
		if i == 0 {
			kr.Current = id
		}
	}
	return kr, nil
}

// aad: length-prefixed fields, so no value can pretend to be the boundary between two others.
func aad(id, scope, origin, login string) []byte {
	var out []byte
	for _, f := range []string{"harnessly-vault/v1", id, scope, origin, login} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(f)))
		out = append(append(out, n[:]...), f...)
	}
	return out
}

func (k *Keyring) seal(plain string, ad []byte) (sealed, error) {
	key, ok := k.Keys[k.Current]
	if !ok {
		return sealed{}, fmt.Errorf("no current key %s", k.Current)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return sealed{}, err
	}
	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return sealed{}, err
	}
	return sealed{KeyID: k.Current, Nonce: nonce, Secret: gcm.Seal(nil, nonce, []byte(plain), ad)}, nil
}

func (k *Keyring) open(s sealed, ad []byte) (string, error) {
	key, ok := k.Keys[s.KeyID]
	if !ok {
		return "", fmt.Errorf("no key %s", s.KeyID)
	}
	if len(s.Nonce) != nonceBytes || len(s.Secret) < tagBytes {
		return "", errors.New("damaged entry")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, s.Nonce, s.Secret, ad)
	if err != nil {
		return "", errors.New("entry does not open")
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
