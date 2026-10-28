// Package chatmove carries a conversation to another machine of the same person: the provider-neutral
// history leaves the source in an encrypted bundle, the target starts a new native session on it in a worktree of its
// own, and the source is sealed read-only once the cluster has switched. Nothing here names the source's account,
// provider, model, session or machine: the bundle is the history and where in the repository it was made.
package chatmove

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	// BundleVersion is the version of the bundle's shape.
	BundleVersion = 1
	// MaxHistory bounds the history a bundle carries; a longer one is refused instead of cut.
	MaxHistory = 32 << 20
	// MaxTitle bounds the title in characters.
	MaxTitle = 300
	// MaxBody bounds an encrypted bundle in the request that carries it.
	MaxBody = MaxHistory + MaxHistory/2 + 1<<16
)

// Project is where in a repository the conversation worked: the repository by its normalised remote, the folder inside
// it and the commit its tree was at. It is how a target finds the same files; it carries no path of the source.
type Project struct {
	RepoKey string `json:"repoKey"`
	RelDir  string `json:"relDir"`
	Commit  string `json:"commit"`
}

// Bundle is everything a move carries. The field set is closed: a bundle with any other field is refused, so an
// identity cannot ride along by accident.
type Bundle struct {
	V       int      `json:"v"`
	Title   string   `json:"title"`
	History string   `json:"history"`
	Project *Project `json:"project"`
}

// ErrBundle: the bundle is not one this runtime accepts.
var ErrBundle = errors.New("chatmove: the bundle is not valid")

// Validate says whether the bundle can be imported.
func (b Bundle) Validate() error {
	switch {
	case b.V != BundleVersion, len(b.History) == 0, len(b.History) > MaxHistory, !utf8.ValidString(b.History),
		utf8.RuneCountInString(b.Title) > MaxTitle:
		return ErrBundle
	case b.Project != nil && (b.Project.RepoKey == "" || b.Project.Commit == "" || !validCommit(b.Project.Commit) || unsafeRel(b.Project.RelDir)):
		return ErrBundle
	}
	return nil
}

func validCommit(c string) bool {
	if len(c) < 7 || len(c) > 64 {
		return false
	}
	for _, r := range c {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// DecodeBundle reads a bundle's JSON and refuses any field outside the closed set.
func DecodeBundle(raw []byte) (Bundle, error) {
	var b Bundle
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Bundle{}, ErrBundle
	}
	if dec.More() {
		return Bundle{}, ErrBundle
	}
	return b, b.Validate()
}

// Direction says which end of the transport an envelope is made for; it is part of the key, so what a source wrote
// cannot be fed back to a source.
type Direction string

const (
	// Export is what a source produces; Import is what a target consumes.
	Export Direction = "export"
	Import Direction = "import"
)

// Envelope is a bundle in transit: AES-256-GCM under a key derived from the pair's secret and the operation.
type Envelope struct {
	Enc   string `json:"enc"`
	Nonce string `json:"nonce"`
	CT    string `json:"ct"`
}

const encName = "aes-256-gcm"

func aead(secret []byte, opID string, dir Direction) (cipher.AEAD, error) {
	if len(secret) == 0 || opID == "" {
		return nil, errors.New("chatmove: no key")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "harnessly/chat-move/v1|"+opID+"|"+string(dir), 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt seals plain for the operation.
func Encrypt(secret []byte, opID string, dir Direction, plain []byte) (Envelope, error) {
	g, err := aead(secret, opID, dir)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, err
	}
	ct := g.Seal(nil, nonce, plain, []byte(opID))
	return Envelope{Enc: encName, Nonce: base64.StdEncoding.EncodeToString(nonce), CT: base64.StdEncoding.EncodeToString(ct)}, nil
}

// Decrypt opens an envelope made for the operation; any change to it, another key or another operation fails.
func Decrypt(secret []byte, opID string, dir Direction, e Envelope) ([]byte, error) {
	if e.Enc != encName {
		return nil, ErrBundle
	}
	g, err := aead(secret, opID, dir)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(e.Nonce)
	if err != nil || len(nonce) != g.NonceSize() {
		return nil, ErrBundle
	}
	ct, err := base64.StdEncoding.DecodeString(e.CT)
	if err != nil {
		return nil, ErrBundle
	}
	plain, err := g.Open(nil, nonce, ct, []byte(opID))
	if err != nil {
		return nil, ErrBundle
	}
	return plain, nil
}
