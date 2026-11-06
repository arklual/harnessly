package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The manifest is signed with Ed25519 over its exact bytes; the detached signature file (release.json.sig) is
//
//	{"alg":"ed25519","key_id":"<first 8 hex of sha256(public key)>","sig":"<base64>"}
//
// The public key is pinned by the person who installs (a file of one line, the base64 of the 32 bytes): a manifest that
// the hub itself serves is not trusted just because the hub served it. The release's cosign signature of checksums.txt
// stays as it is for those who verify the archives.

// Signature is the content of release.json.sig.
type Signature struct {
	Alg   string `json:"alg"`
	KeyID string `json:"key_id"`
	Sig   string `json:"sig"`
}

// KeyID names a public key.
func KeyID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:4])
}

// Sign signs the manifest bytes.
func Sign(priv ed25519.PrivateKey, body []byte) ([]byte, error) {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("not an ed25519 key")
	}
	return json.Marshal(Signature{Alg: "ed25519", KeyID: KeyID(pub), Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))})
}

// ErrBadSignature: the manifest is not the one the key signed.
var ErrBadSignature = errors.New("release manifest: the signature does not match")

// Verify checks the manifest bytes against the signature file and the pinned key.
func Verify(pub ed25519.PublicKey, body, sigFile []byte) error {
	var s Signature
	if err := json.Unmarshal(sigFile, &s); err != nil {
		return fmt.Errorf("release signature: %w", err)
	}
	if s.Alg != "ed25519" {
		return fmt.Errorf("release signature: algorithm %q is not supported", s.Alg)
	}
	if s.KeyID != KeyID(pub) {
		return fmt.Errorf("release signature: made with key %s, the pinned key is %s", s.KeyID, KeyID(pub))
	}
	raw, err := base64.StdEncoding.DecodeString(s.Sig)
	if err != nil || len(raw) != ed25519.SignatureSize || !ed25519.Verify(pub, body, raw) {
		return ErrBadSignature
	}
	return nil
}

// ParsePublicKey reads the pinned key file (base64 of the 32 bytes).
func ParsePublicKey(text string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("release key: expected the base64 of a 32-byte ed25519 public key")
	}
	return ed25519.PublicKey(raw), nil
}

// ParsePrivateKey reads a private key file (base64 of the 64 bytes), for the tool that signs a release.
func ParsePrivateKey(text string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("release key: expected the base64 of a 64-byte ed25519 private key")
	}
	return ed25519.PrivateKey(raw), nil
}
