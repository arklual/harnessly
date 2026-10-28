package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

// softKey is a software authenticator: one ES256 credential, "none" attestation, user verification on.
type softKey struct {
	t         *testing.T
	origin    string
	rpID      string
	id        []byte
	priv      *ecdsa.PrivateKey
	userID    []byte
	signCount uint32
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64url %q: %v", s, err)
	}
	return b
}

// cbor writes the few CBOR shapes a WebAuthn attestation uses.
func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 256:
		return []byte{major<<5 | 24, byte(n)}
	default:
		b := make([]byte, 3)
		b[0] = major<<5 | 25
		binary.BigEndian.PutUint16(b[1:], uint16(n))
		return b
	}
}
func cborInt(n int64) []byte {
	if n >= 0 {
		return cborHead(0, uint64(n))
	}
	return cborHead(1, uint64(-1-n))
}
func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }
func cborText(s string) []byte  { return append(cborHead(3, uint64(len(s))), s...) }
func cborMap(pairs ...[]byte) []byte {
	out := cborHead(5, uint64(len(pairs)/2))
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

func (k *softKey) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": k.origin, "crossOrigin": false})
	return b
}

func (k *softKey) rpHash() []byte { h := sha256.Sum256([]byte(k.rpID)); return h[:] }

// create answers PublicKeyCredentialCreationOptions (as the browser would hand them to the authenticator).
func (k *softKey) create(options map[string]any) map[string]any {
	k.t.Helper()
	challenge := options["challenge"].(string)
	user := options["user"].(map[string]any)
	k.userID = unb64(k.t, user["id"].(string))
	k.id = make([]byte, 32)
	rand.Read(k.id)
	var err error
	if k.priv, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		k.t.Fatal(err)
	}
	cose := cborMap(
		cborInt(1), cborInt(2), // kty EC2
		cborInt(3), cborInt(-7), // alg ES256
		cborInt(-1), cborInt(1), // crv P-256
		cborInt(-2), cborBytes(k.priv.X.FillBytes(make([]byte, 32))),
		cborInt(-3), cborBytes(k.priv.Y.FillBytes(make([]byte, 32))),
	)
	var auth bytes.Buffer
	auth.Write(k.rpHash())
	auth.WriteByte(0x45) // UP | UV | AT
	_ = binary.Write(&auth, binary.BigEndian, uint32(0))
	auth.Write(make([]byte, 16)) // AAGUID
	_ = binary.Write(&auth, binary.BigEndian, uint16(len(k.id)))
	auth.Write(k.id)
	auth.Write(cose)
	att := cborMap(cborText("fmt"), cborText("none"), cborText("attStmt"), cborMap(), cborText("authData"), cborBytes(auth.Bytes()))
	return map[string]any{
		"id": b64(k.id), "rawId": b64(k.id), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": b64(k.clientData("webauthn.create", challenge)), "attestationObject": b64(att)},
		"clientExtensionResults": map[string]any{},
	}
}

// get answers PublicKeyCredentialRequestOptions.
func (k *softKey) get(options map[string]any) map[string]any {
	k.t.Helper()
	challenge := options["challenge"].(string)
	k.signCount++
	var auth bytes.Buffer
	auth.Write(k.rpHash())
	auth.WriteByte(0x05) // UP | UV
	_ = binary.Write(&auth, binary.BigEndian, k.signCount)
	cd := k.clientData("webauthn.get", challenge)
	sum := sha256.Sum256(cd)
	signed := sha256.Sum256(append(append([]byte{}, auth.Bytes()...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, signed[:])
	if err != nil {
		k.t.Fatal(err)
	}
	return map[string]any{
		"id": b64(k.id), "rawId": b64(k.id), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": b64(cd), "authenticatorData": b64(auth.Bytes()), "signature": b64(sig), "userHandle": b64(k.userID)},
		"clientExtensionResults": map[string]any{},
	}
}

func newSoftKey(e *testEnv) *softKey {
	return &softKey{t: e.t, origin: e.srv.URL, rpID: "localhost"}
}

type ceremony struct {
	ChallengeID string         `json:"challengeId"`
	PublicKey   map[string]any `json:"publicKey"`
}

func TestPasskeyRegisterAndSignIn(t *testing.T) {
	e := newEnv(t)
	e.signIn("pk@example.com")
	key := newSoftKey(e)

	var begin ceremony
	r := e.post("/api/me/passkeys/begin", map[string]any{})
	if r.StatusCode != 200 {
		t.Fatalf("begin: %d %s", r.StatusCode, r.Body)
	}
	r.json(&begin)
	sel, _ := begin.PublicKey["authenticatorSelection"].(map[string]any)
	if sel["residentKey"] != "required" || begin.PublicKey["rp"].(map[string]any)["id"] != "localhost" {
		t.Errorf("creation options: %v", begin.PublicKey)
	}
	r = e.post("/api/me/passkeys/finish", map[string]any{"challengeId": begin.ChallengeID, "credential": key.create(begin.PublicKey), "name": "Ноутбук"})
	if r.StatusCode != 200 {
		t.Fatalf("finish: %d %s", r.StatusCode, r.Body)
	}
	var added struct{ Passkey PasskeyView }
	r.json(&added)
	if added.Passkey.Name != "Ноутбук" || added.Passkey.LastUsedAt != nil {
		t.Errorf("passkey: %+v", added)
	}
	// a ceremony is used once
	if r := e.post("/api/me/passkeys/finish", map[string]any{"challengeId": begin.ChallengeID, "credential": newSoftKey(e).create(begin.PublicKey)}); r.StatusCode != 410 {
		t.Errorf("replayed ceremony: %d", r.StatusCode)
	}

	// signed out, the passkey signs in without a name or an email
	browser := e.newClient()
	r = e.do(browser, "POST", "/api/auth/passkey/login/begin", map[string]any{})
	if r.StatusCode != 200 {
		t.Fatalf("login begin: %d %s", r.StatusCode, r.Body)
	}
	var login ceremony
	r.json(&login)
	if allowed, _ := login.PublicKey["allowCredentials"].([]any); login.PublicKey["userVerification"] != "preferred" || len(allowed) != 0 {
		t.Errorf("request options: %v", login.PublicKey)
	}
	r = e.do(browser, "POST", "/api/auth/passkey/login/finish", map[string]any{"challengeId": login.ChallengeID, "credential": key.get(login.PublicKey)})
	if r.StatusCode != 200 {
		t.Fatalf("login finish: %d %s\n%s", r.StatusCode, r.Body, e.logs)
	}
	var in SignedIn
	r.json(&in)
	if in.User.Email != "pk@example.com" || in.IsNew || in.Next != "/new" {
		t.Errorf("signed in: %+v", in)
	}
	if r := e.do(browser, "GET", "/api/me", nil); r.StatusCode != 200 {
		t.Errorf("session after passkey: %d", r.StatusCode)
	}
	var me struct{ Passkeys []PasskeyView }
	e.do(browser, "GET", "/api/me", nil).json(&me)
	if len(me.Passkeys) != 1 || me.Passkeys[0].LastUsedAt == nil {
		t.Errorf("last used not recorded: %+v", me.Passkeys)
	}

	// a replayed assertion is refused (ceremony consumed), and so is a signature by another key
	if r := e.do(e.newClient(), "POST", "/api/auth/passkey/login/finish", map[string]any{"challengeId": login.ChallengeID, "credential": key.get(login.PublicKey)}); r.StatusCode != 410 {
		t.Errorf("replayed login: %d", r.StatusCode)
	}
	r = e.do(e.newClient(), "POST", "/api/auth/passkey/login/begin", map[string]any{})
	r.json(&login)
	forged := *key
	forged.priv, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if r := e.do(e.newClient(), "POST", "/api/auth/passkey/login/finish", map[string]any{"challengeId": login.ChallengeID, "credential": forged.get(login.PublicKey)}); r.StatusCode != 401 {
		t.Errorf("forged signature: %d %s", r.StatusCode, r.Body)
	}
	// ... and an assertion for another origin
	r = e.do(e.newClient(), "POST", "/api/auth/passkey/login/begin", map[string]any{})
	r.json(&login)
	foreign := *key
	foreign.origin = "https://evil.example"
	if r := e.do(e.newClient(), "POST", "/api/auth/passkey/login/finish", map[string]any{"challengeId": login.ChallengeID, "credential": foreign.get(login.PublicKey)}); r.StatusCode != 401 {
		t.Errorf("foreign origin: %d", r.StatusCode)
	}

	// the same authenticator cannot be added twice (the creation options exclude it)
	r = e.post("/api/me/passkeys/begin", map[string]any{})
	r.json(&begin)
	if ex, _ := begin.PublicKey["excludeCredentials"].([]any); len(ex) != 1 {
		t.Errorf("excludeCredentials: %v", begin.PublicKey["excludeCredentials"])
	}

	// the passkey can go while another way remains; and it can be deleted only by its owner
	if r := e.do(e.client, "DELETE", "/api/me/passkeys/"+added.Passkey.ID, nil); r.StatusCode != 204 {
		t.Errorf("delete: %d %s", r.StatusCode, r.Body)
	}
	if r := e.do(e.client, "DELETE", "/api/me/passkeys/"+added.Passkey.ID, nil); r.StatusCode != 404 {
		t.Errorf("delete twice: %d", r.StatusCode)
	}
}

func TestPasskeyOffOnAnIPHost(t *testing.T) {
	e := newEnv(t, withEnv("HUB_PASSKEY", "off"))
	if e.svc.PasskeyOn() {
		t.Error("HUB_PASSKEY=off is ignored")
	}
	for _, path := range []string{"/api/auth/passkey/login/begin"} {
		if r := e.post(path, map[string]any{}); r.StatusCode != 422 || r.errCode() != "provider_off" {
			t.Errorf("%s: %d %s", path, r.StatusCode, r.Body)
		}
	}
	e.signIn("x@example.com")
	if r := e.post("/api/me/passkeys/begin", map[string]any{}); r.StatusCode != 422 {
		t.Errorf("register while off: %d", r.StatusCode)
	}

	// a hub reached by IP cannot do WebAuthn: it starts, with passkeys off and a warning
	e = newEnv(t, withEnv("HUB_PUBLIC_URL", "http://203.0.113.5:8040"))
	if e.svc.PasskeyOn() || !strings.Contains(e.logs.String(), "passkeys are off") {
		t.Errorf("IP host: on=%v log=%s", e.svc.PasskeyOn(), e.logs)
	}
}

func TestPasskeyRegistrationNeedsASession(t *testing.T) {
	e := newEnv(t)
	if r := e.post("/api/me/passkeys/begin", map[string]any{}); r.StatusCode != 401 {
		t.Errorf("%d", r.StatusCode)
	}
	// someone else's ceremony cannot be finished
	e.signIn("one@example.com")
	var begin ceremony
	e.post("/api/me/passkeys/begin", map[string]any{}).json(&begin)
	other := e.newClient()
	e.signInWith(other, "two@example.com")
	key := newSoftKey(e)
	r := e.do(other, "POST", "/api/me/passkeys/finish", map[string]any{"challengeId": begin.ChallengeID, "credential": key.create(begin.PublicKey)})
	if r.StatusCode != 403 {
		t.Errorf("another user's ceremony: %d %s", r.StatusCode, r.Body)
	}
}
