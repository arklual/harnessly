package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// VAPIDFile is the key pair's file in the data folder.
const VAPIDFile = "vapid-keys.json"

// VAPIDKeys are the application server's key pair as the file holds it: base64url without padding, the public key
// uncompressed (65 bytes), the private key the raw scalar (32 bytes). Every runtime of a person must use the same pair:
// a browser subscribes once, against one key.
type VAPIDKeys struct {
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
}

var b64 = base64.RawURLEncoding

// LoadOrCreateVAPID reads the key pair from dir, or creates and stores one when the file is missing or unreadable (0600,
// through a temporary file).
func LoadOrCreateVAPID(dir string) (VAPIDKeys, error) {
	path := filepath.Join(dir, VAPIDFile)
	if raw, err := os.ReadFile(path); err == nil {
		var k VAPIDKeys
		if json.Unmarshal(raw, &k) == nil {
			return k, nil
		}
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return VAPIDKeys{}, err
	}
	d, err := priv.Bytes()
	if err != nil {
		return VAPIDKeys{}, err
	}
	pub, err := priv.PublicKey.Bytes()
	if err != nil {
		return VAPIDKeys{}, err
	}
	k := VAPIDKeys{PublicKey: b64.EncodeToString(pub), PrivateKey: b64.EncodeToString(d)}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return k, err
	}
	raw, _ := json.MarshalIndent(k, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		_ = os.Remove(tmp)
		return k, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return k, err
	}
	return k, nil
}

// signer is a parsed VAPID key pair.
type signer struct {
	key    *ecdsa.PrivateKey
	public string
}

func (k VAPIDKeys) signer() (*signer, error) {
	d, err := b64.DecodeString(k.PrivateKey)
	if err != nil {
		return nil, errors.New("push: bad VAPID private key")
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
	if err != nil {
		return nil, err
	}
	pub, err := priv.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	return &signer{key: priv, public: b64.EncodeToString(pub)}, nil
}

// vapidExpiry is how long a VAPID token lasts (RFC 8292 allows at most 24 h; the old library used 12).
const vapidExpiry = 12 * time.Hour

// authorization is the `vapid t=<JWT>, k=<public key>` header of RFC 8292 for a push service endpoint.
func (s *signer) authorization(endpoint, subject string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("push: bad endpoint")
	}
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{u.Scheme + "://" + u.Host, now.Add(vapidExpiry).Unix(), subject})
	unsigned := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, sv, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return "vapid t=" + unsigned + "." + b64.EncodeToString(sig) + ", k=" + s.public, nil
}

var (
	urlPadded = base64.URLEncoding
	stdPadded = base64.StdEncoding
	stdRaw    = base64.RawStdEncoding
)
