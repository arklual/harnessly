package hubproto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DialScheme is the Authorization scheme of the signed dial.
	DialScheme = "Harnessly-Ed25519"
	// DialDomain is the first line of the signed message: a signature made for the dial is good for nothing else.
	DialDomain = "harnessly-agent-dial-v1"
	// UnlinkDomain is the first line of the message signed to unlink a machine from its hub: a signature made for the
	// dial cannot remove a machine, nor the other way round.
	UnlinkDomain = "harnessly-agent-unlink-v1"
	// SitesDomain is the first line of the message signed to ask the hub for a publishing session of the machine's
	// space: a signature made for the dial or the unlink is good for nothing here, and this one for nothing else.
	SitesDomain = "harnessly-agent-sites-v1"
	// DialWindow is how far the dial's time may be from the hub's clock, either way.
	DialWindow = 120 * time.Second
	// NonceMemory is how long the hub remembers a nonce of a machine: a dial replayed inside it is refused.
	NonceMemory = 5 * time.Minute

	nonceBytes = 16
)

var (
	errDialHeader = errors.New("bad dial header")
	errDialTime   = errors.New("dial time is outside the allowed window")
	errDialSig    = errors.New("dial signature does not match")

	b64 = base64.RawURLEncoding
)

// DialAuth is a parsed Authorization header of a dial.
type DialAuth struct {
	MachineID string
	// T is the agent's clock, unix seconds.
	T     int64
	Nonce string
	Sig   string
}

// DialMessage is what the agent signs: the domain, the machine, the time, the nonce and the host it dials, one per line.
// host is the one DialHost gives.
func DialMessage(machineID string, t int64, nonce, host string) []byte {
	return signedMessage(DialDomain, machineID, t, nonce, host)
}

func signedMessage(domain, machineID string, t int64, nonce, host string) []byte {
	return []byte(strings.Join([]string{domain, machineID, strconv.FormatInt(t, 10), nonce, host}, "\n"))
}

// DialHost is the host of a dial URL as both sides spell it in the signature: lower-case, with the port only when it is
// not the scheme's own (80 for ws and http, 443 for wss and https).
func DialHost(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("not a dial url: %q", rawURL)
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	switch {
	case port == "":
	case port == "80" && (u.Scheme == "ws" || u.Scheme == "http"):
	case port == "443" && (u.Scheme == "wss" || u.Scheme == "https"):
	default:
		host += ":" + port
	}
	return host, nil
}

// SignDial makes the Authorization header value for a dial to host (see DialHost) at now, with a fresh nonce.
func SignDial(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(DialDomain, priv, machineID, now, host)
}

// SignUnlink makes the Authorization header value of the request that removes the machine from its hub. It has the
// shape of a dial's, signed under UnlinkDomain.
func SignUnlink(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(UnlinkDomain, priv, machineID, now, host)
}

// SignSites makes the Authorization header value of the request for a publishing session (POST /agent/sites).
func SignSites(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(SitesDomain, priv, machineID, now, host)
}

func signHeader(domain string, priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", errors.New("not an Ed25519 private key")
	}
	raw := make([]byte, nonceBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	nonce := b64.EncodeToString(raw)
	t := now.Unix()
	sig := ed25519.Sign(priv, signedMessage(domain, machineID, t, nonce, strings.ToLower(host)))
	return fmt.Sprintf("%s m=%s, t=%d, n=%s, s=%s", DialScheme, machineID, t, nonce, b64.EncodeToString(sig)), nil
}

// ParseDialHeader reads an Authorization header value made by SignDial. It checks the shape, not the signature.
func ParseDialHeader(h string) (DialAuth, error) {
	rest, ok := strings.CutPrefix(h, DialScheme+" ")
	if !ok {
		return DialAuth{}, errDialHeader
	}
	var a DialAuth
	seen := map[string]bool{}
	for _, part := range strings.Split(rest, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || v == "" || seen[k] {
			return DialAuth{}, errDialHeader
		}
		seen[k] = true
		switch k {
		case "m":
			a.MachineID = v
		case "t":
			t, err := strconv.ParseInt(v, 10, 64)
			if err != nil || t <= 0 {
				return DialAuth{}, errDialHeader
			}
			a.T = t
		case "n":
			a.Nonce = v
		case "s":
			a.Sig = v
		default:
			return DialAuth{}, errDialHeader
		}
	}
	if len(seen) != 4 || !validToken(a.MachineID) || !validToken(a.Nonce) || !validToken(a.Sig) {
		return DialAuth{}, errDialHeader
	}
	if raw, err := b64.DecodeString(a.Sig); err != nil || len(raw) != ed25519.SignatureSize {
		return DialAuth{}, errDialHeader
	}
	if raw, err := b64.DecodeString(a.Nonce); err != nil || len(raw) != nonceBytes {
		return DialAuth{}, errDialHeader
	}
	return a, nil
}

// validToken: a value that cannot break the header apart nor the signed lines.
func validToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// VerifyDial checks the signature of a parsed dial against the machine's public key and the host the hub is reached
// at, and the time window. The nonce memory is the hub's.
func VerifyDial(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(DialDomain, pub, a, host, now)
}

// VerifyUnlink is VerifyDial for the header SignUnlink made.
func VerifyUnlink(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(UnlinkDomain, pub, a, host, now)
}

// VerifySites is VerifyDial for the header SignSites made.
func VerifySites(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(SitesDomain, pub, a, host, now)
}

func verifyHeader(domain string, pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	if len(pub) != ed25519.PublicKeySize {
		return errDialSig
	}
	skew := now.Sub(time.Unix(a.T, 0))
	if skew < -DialWindow || skew > DialWindow {
		return errDialTime
	}
	sig, err := b64.DecodeString(a.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errDialHeader
	}
	if !ed25519.Verify(pub, signedMessage(domain, a.MachineID, a.T, a.Nonce, strings.ToLower(host)), sig) {
		return errDialSig
	}
	return nil
}
