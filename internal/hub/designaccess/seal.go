package designaccess

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// A guest link's address is kept sealed with a key derived from the hub's own secret (HUB_SECRET_KEY, or the key file
// in the data directory), so whoever runs the access can copy it again; only its hash opens the link. A hub whose key
// changed simply cannot open the older addresses: those links are reissued.

const linkKeyInfo = "harnessly-access-link/v1"

type sealer struct {
	id   string
	aead cipher.AEAD
}

// newSealer derives the links' key from the hub's master key; nil without one.
func newSealer(master []byte) *sealer {
	if len(master) == 0 {
		return nil
	}
	key, err := hkdf.Key(sha256.New, master, []byte("harnessly"), linkKeyInfo, 32)
	if err != nil {
		return nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(key)
	return &sealer{id: hex.EncodeToString(sum[:4]), aead: aead}
}

func aadOf(linkID string) []byte {
	return []byte(linkKeyInfo + "\x00design_share_links\x00" + linkID)
}

// seal is the token sealed for one link: "<key id>.<nonce>.<sealed>"; "" without a key.
func (s *sealer) seal(linkID, token string) string {
	if s == nil {
		return ""
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ""
	}
	out := s.aead.Seal(nil, nonce, []byte(token), aadOf(linkID))
	enc := base64.RawURLEncoding
	return s.id + "." + enc.EncodeToString(nonce) + "." + enc.EncodeToString(out)
}

// open is the token back; false for another key, another link or a damaged value.
func (s *sealer) open(linkID, sealed string) (string, bool) {
	if s == nil || sealed == "" {
		return "", false
	}
	parts := strings.Split(sealed, ".")
	if len(parts) != 3 || parts[0] != s.id {
		return "", false
	}
	enc := base64.RawURLEncoding
	nonce, err := enc.DecodeString(parts[1])
	if err != nil || len(nonce) != s.aead.NonceSize() {
		return "", false
	}
	box, err := enc.DecodeString(parts[2])
	if err != nil {
		return "", false
	}
	plain, err := s.aead.Open(nil, nonce, box, aadOf(linkID))
	if err != nil {
		return "", false
	}
	return string(plain), true
}
