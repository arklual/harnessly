package hubproto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

const (
	envelopeInfo = "harnessly-envelope-v1"
	// MaxSealed is the longest plaintext an envelope carries.
	MaxSealed = 8 * 1024

	x25519Size = 32
	gcmNonce   = 12
	gcmTag     = 16
)

// ErrEnvelope: the envelope does not open here. It does not say why, so a hub (or anyone) learns nothing from trying.
var ErrEnvelope = errors.New("the envelope does not open")

// Envelope is a key or a secret sealed in a browser to a machine's X25519 key. All three fields are base64 (standard,
// padded); ct has the AES-GCM tag at its end.
type Envelope struct {
	V     int    `json:"v"`
	Epk   string `json:"epk"`
	Nonce string `json:"nonce"`
	Ct    string `json:"ct"`
}

// SealAAD is the additional data an envelope is bound to: the machine and what the plaintext is for (`key:<provider>`,
// `env:<NAME>`), so an envelope made for one machine or purpose does not open as another.
func SealAAD(machineID, purpose string) []byte {
	return []byte("harnessly:" + machineID + ":" + purpose)
}

func deriveKey(shared, epk, encPub []byte) ([]byte, error) {
	salt := append(append([]byte{}, epk...), encPub...)
	return hkdf.Key(sha256.New, shared, salt, envelopeInfo, 32)
}

func aead(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal seals plaintext to the machine's X25519 public key with a fresh ephemeral key and nonce.
func Seal(encPub []byte, machineID, purpose string, plaintext []byte) (Envelope, error) {
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, gcmNonce)
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	return sealWith(eph, nonce, encPub, machineID, purpose, plaintext)
}

// sealWith is Seal with the ephemeral key and the nonce given: the test vectors fix them.
func sealWith(eph *ecdh.PrivateKey, nonce, encPub []byte, machineID, purpose string, plaintext []byte) (Envelope, error) {
	if len(plaintext) > MaxSealed {
		return Envelope{}, errors.New("the plaintext is too long to seal")
	}
	pub, err := ecdh.X25519().NewPublicKey(encPub)
	if err != nil {
		return Envelope{}, err
	}
	shared, err := eph.ECDH(pub)
	if err != nil {
		return Envelope{}, err
	}
	epk := eph.PublicKey().Bytes()
	key, err := deriveKey(shared, epk, encPub)
	if err != nil {
		return Envelope{}, err
	}
	gcm, err := aead(key)
	if err != nil {
		return Envelope{}, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, SealAAD(machineID, purpose))
	std := base64.StdEncoding
	return Envelope{V: 1, Epk: std.EncodeToString(epk), Nonce: std.EncodeToString(nonce), Ct: std.EncodeToString(ct)}, nil
}

// Open opens an envelope with the machine's X25519 private key. Any failure is ErrEnvelope.
func Open(encPriv []byte, machineID, purpose string, e Envelope) ([]byte, error) {
	std := base64.StdEncoding
	epk, err1 := std.DecodeString(e.Epk)
	nonce, err2 := std.DecodeString(e.Nonce)
	ct, err3 := std.DecodeString(e.Ct)
	if e.V != 1 || err1 != nil || err2 != nil || err3 != nil || len(epk) != x25519Size || len(nonce) != gcmNonce ||
		len(ct) < gcmTag || len(ct) > MaxSealed+gcmTag {
		return nil, ErrEnvelope
	}
	priv, err := ecdh.X25519().NewPrivateKey(encPriv)
	if err != nil {
		return nil, ErrEnvelope
	}
	pub, err := ecdh.X25519().NewPublicKey(epk)
	if err != nil {
		return nil, ErrEnvelope
	}
	shared, err := priv.ECDH(pub)
	if err != nil {
		return nil, ErrEnvelope
	}
	key, err := deriveKey(shared, epk, priv.PublicKey().Bytes())
	if err != nil {
		return nil, ErrEnvelope
	}
	gcm, err := aead(key)
	if err != nil {
		return nil, ErrEnvelope
	}
	plain, err := gcm.Open(nil, nonce, ct, SealAAD(machineID, purpose))
	if err != nil {
		return nil, ErrEnvelope
	}
	return plain, nil
}
