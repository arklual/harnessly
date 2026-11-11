// Package apptoken is the platform's token for an app's backend (docs/build-apps/10-auth.md): a short JWT the host puts
// in the calls to an app's MCP server so the server knows who calls, for which app, without a sign-in of its own. The
// hub signs it with one Ed25519 key of the deployment and publishes the public half at /.well-known/jwks.json; a machine
// without a hub signs with its own key. A backend checks the signature, the issuer, its own audience and the expiry.
//
// Nothing here holds a person's password or an identity provider's token: the token says who the hub signed in, and
// lives minutes.
package apptoken

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// TTL is how long a token lives; MaxTTL bounds what a caller may ask.
const (
	TTL    = 5 * time.Minute
	MaxTTL = 10 * time.Minute
	// Type is the JWT's typ header: a backend may insist on it, so an ID token of the same issuer is never taken for it.
	Type = "harnessly-app+jwt"
	// JWKSPath is where a hub (and a machine without one) publishes its public keys.
	JWKSPath = "/.well-known/jwks.json"
	// DiscoveryPath is the OpenID discovery document of the issuer: its iss and where its keys are, so a backend checks
	// the tokens with a stock OIDC library.
	DiscoveryPath = "/.well-known/openid-configuration"
)

// Claims are what the token says. Subject is the person's stable id at the issuer; Email and Name are for display and
// logs, not for access decisions unless the backend trusts the issuer's emails. Org and Space are the person's
// organization and team on the hub (empty on a machine without one). App and Server name the app and its MCP server the
// call is for; Audience is the backend the token is good for, and nothing else.
type Claims struct {
	Issuer   string    `json:"iss"`
	Subject  string    `json:"sub"`
	Audience string    `json:"aud"`
	Email    string    `json:"email,omitempty"`
	Name     string    `json:"name,omitempty"`
	Org      string    `json:"org,omitempty"`
	Space    string    `json:"space,omitempty"`
	App      string    `json:"app"`
	Server   string    `json:"server,omitempty"`
	Machine  string    `json:"machine,omitempty"`
	IssuedAt time.Time `json:"-"`
	Expiry   time.Time `json:"-"`
	ID       string    `json:"-"`
}

// wire is Claims as JSON: the registered claims through go-jose, ours beside them.
type wire struct {
	jwt.Claims
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Org     string `json:"org,omitempty"`
	Space   string `json:"space,omitempty"`
	App     string `json:"app"`
	Server  string `json:"server,omitempty"`
	Machine string `json:"machine,omitempty"`
}

// Errors of Mint and Verify.
var (
	ErrNoAudience = errors.New("apptoken: a token needs an audience")
	ErrNoSubject  = errors.New("apptoken: a token needs a subject")
	ErrInvalid    = errors.New("apptoken: the token is not valid")
)

// KeyID is the key's id in the JWKS: the base64url SHA-256 of the public key, cut to 16 characters. The same key has
// the same id on every replica of a hub.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:])[:16]
}

// Signer mints the tokens of one issuer. The first key signs; the others are still published so the tokens they signed
// before a rotation keep verifying until they expire.
type Signer struct {
	issuer string
	key    ed25519.PrivateKey
	others []ed25519.PublicKey
	sig    jose.Signer
	now    func() time.Time
}

// NewSigner is a signer for issuer with key; previous are public keys of earlier rotations, published but not used.
func NewSigner(issuer string, key ed25519.PrivateKey, previous ...ed25519.PublicKey) (*Signer, error) {
	if issuer == "" || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("apptoken: a signer needs an issuer and an Ed25519 key")
	}
	pub := key.Public().(ed25519.PublicKey)
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: jose.JSONWebKey{Key: key, KeyID: KeyID(pub)}},
		(&jose.SignerOptions{}).WithType(Type))
	if err != nil {
		return nil, err
	}
	return &Signer{issuer: issuer, key: key, others: previous, sig: sig, now: time.Now}, nil
}

// SetClock replaces the signer's clock (tests).
func (s *Signer) SetClock(now func() time.Time) { s.now = now }

// Issuer is the iss of the signer's tokens.
func (s *Signer) Issuer() string { return s.issuer }

// Mint signs c for ttl (TTL when 0, at most MaxTTL). The issuer, issue time, expiry and id are the signer's.
func (s *Signer) Mint(c Claims, ttl time.Duration) (string, Claims, error) {
	if c.Audience == "" {
		return "", Claims{}, ErrNoAudience
	}
	if c.Subject == "" {
		return "", Claims{}, ErrNoSubject
	}
	if ttl <= 0 {
		ttl = TTL
	}
	ttl = min(ttl, MaxTTL)
	var id [12]byte
	_, _ = rand.Read(id[:])
	now := s.now().UTC().Truncate(time.Second)
	c.Issuer, c.IssuedAt, c.Expiry, c.ID = s.issuer, now, now.Add(ttl), base64.RawURLEncoding.EncodeToString(id[:])
	w := wire{
		Claims: jwt.Claims{Issuer: c.Issuer, Subject: c.Subject, Audience: jwt.Audience{c.Audience}, ID: c.ID,
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(c.Expiry)},
		Email: c.Email, Name: c.Name, Org: c.Org, Space: c.Space, App: c.App, Server: c.Server, Machine: c.Machine,
	}
	raw, err := jwt.Signed(s.sig).Claims(w).Serialize()
	if err != nil {
		return "", Claims{}, err
	}
	return raw, c, nil
}

// JWKS is the public keys as a JSON Web Key Set: the signing one first.
func (s *Signer) JWKS() []byte {
	keys := []jose.JSONWebKey{publicJWK(s.key.Public().(ed25519.PublicKey))}
	for _, p := range s.others {
		keys = append(keys, publicJWK(p))
	}
	out, _ := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	return out
}

func publicJWK(pub ed25519.PublicKey) jose.JSONWebKey {
	return jose.JSONWebKey{Key: pub, KeyID: KeyID(pub), Algorithm: string(jose.EdDSA), Use: "sig"}
}

// Verify checks a token against a JWKS the way a backend should: the EdDSA signature by a key of the set, the typ, the
// issuer, the audience (required) and the expiry with a minute of leeway. It is what the Go example of the docs does.
func Verify(token string, jwks []byte, issuer, audience string, now time.Time) (Claims, error) {
	if audience == "" {
		return Claims{}, ErrNoAudience
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(jwks, &set); err != nil {
		return Claims{}, fmt.Errorf("%w: unreadable key set", ErrInvalid)
	}
	tok, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil || len(tok.Headers) != 1 {
		return Claims{}, fmt.Errorf("%w: not a signed JWT", ErrInvalid)
	}
	h := tok.Headers[0]
	if typ, _ := h.ExtraHeaders[jose.HeaderType].(string); typ != Type {
		return Claims{}, fmt.Errorf("%w: typ is not %s", ErrInvalid, Type)
	}
	keys := set.Key(h.KeyID)
	if len(keys) == 0 {
		return Claims{}, fmt.Errorf("%w: no key %q in the set", ErrInvalid, h.KeyID)
	}
	var w wire
	if err := tok.Claims(keys[0].Key, &w); err != nil {
		return Claims{}, fmt.Errorf("%w: bad signature", ErrInvalid)
	}
	if err := w.Claims.Validate(jwt.Expected{Issuer: issuer, AnyAudience: jwt.Audience{audience}, Time: now}); err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if w.Expiry == nil {
		return Claims{}, fmt.Errorf("%w: no expiry", ErrInvalid)
	}
	c := Claims{Issuer: w.Issuer, Subject: w.Subject, Audience: audience, Email: w.Email, Name: w.Name, Org: w.Org,
		Space: w.Space, App: w.App, Server: w.Server, Machine: w.Machine, Expiry: w.Expiry.Time(), ID: w.ID}
	if w.IssuedAt != nil {
		c.IssuedAt = w.IssuedAt.Time()
	}
	return c, nil
}

// LoadOrCreateKey reads an Ed25519 private key (PKCS#8 PEM) from path, or makes one and writes it there (0600) when the
// file does not exist. Every replica of a hub must read the same file.
func LoadOrCreateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		_, key, gerr := ed25519.GenerateKey(rand.Reader)
		if gerr != nil {
			return nil, gerr
		}
		der, _ := x509.MarshalPKCS8PrivateKey(key)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return nil, err
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseKey(raw)
}

// ParseKey reads an Ed25519 private key from PKCS#8 PEM.
func ParseKey(raw []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("apptoken: the key is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apptoken: the key is not PKCS#8: %w", err)
	}
	key, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("apptoken: the key is not Ed25519")
	}
	return key, nil
}

// ParsePublicKey reads an Ed25519 public key (PKIX PEM): a key of an earlier rotation.
func ParsePublicKey(raw []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("apptoken: the public key is not PEM")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("apptoken: the public key is not Ed25519")
	}
	return pub, nil
}
