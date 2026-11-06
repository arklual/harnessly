package hubproto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// The safe of a machine kept by its hub (docs/safe.md, "The hub's safe"): the runtime asks its machine agent, the agent
// signs the request with the machine's key and posts it to SecretsPath, and the hub reads or writes the owner's folder
// on its Vault-compatible server. A value comes back sealed to the machine's X25519 key, never in the clear.
const (
	// SecretsDomain is the first line of the message signed for a request to the hub's safe.
	SecretsDomain = "harnessly-agent-secrets-v1"
	// SecretsPath is where the hub answers it.
	SecretsPath = "/agent/secrets"
	// MaxSecretsBody bounds the request: the largest value of the safe (64 KiB) with room for its JSON.
	MaxSecretsBody = 160 << 10
	// maxSealedParts bounds the parts an answer is sealed in.
	maxSealedParts = 32
)

// The operations of the hub's safe.
const (
	SecretsList   = "list"
	SecretsRead   = "read"
	SecretsWrite  = "write"
	SecretsDelete = "delete"
	// SecretsRef reads a key an administrator keeps at a path of the server, when the hub lets machines read it.
	SecretsRef = "ref"
)

// SecretsRequest is one request to the hub's safe. Name is a secret of the machine's owner (read, write, delete); Path
// is a folder of the server (ref); Data is what write keeps.
type SecretsRequest struct {
	Op   string            `json:"op"`
	Name string            `json:"name,omitempty"`
	Path string            `json:"path,omitempty"`
	Data map[string]string `json:"data,omitempty"`
}

// SecretsAnswer is the hub's answer: the names (list), or the data that was read, as JSON sealed in parts (read, ref).
type SecretsAnswer struct {
	Names  []string   `json:"names,omitempty"`
	Sealed []Envelope `json:"sealed,omitempty"`
}

// secretsHost is the host line of the signed message with the digest of the body under it, so a signature is good for
// this request only, not for another one sent with the same header.
func secretsHost(host string, body []byte) string {
	sum := sha256.Sum256(body)
	return host + "\n" + hex.EncodeToString(sum[:])
}

// SignSecrets makes the Authorization header value of a request to the hub's safe with this body.
func SignSecrets(priv ed25519.PrivateKey, machineID string, now time.Time, host string, body []byte) (string, error) {
	return signHeader(SecretsDomain, priv, machineID, now, secretsHost(host, body))
}

// VerifySecrets is VerifyDial for the header SignSecrets made over body.
func VerifySecrets(pub ed25519.PublicKey, a DialAuth, host string, now time.Time, body []byte) error {
	return verifyHeader(SecretsDomain, pub, a, secretsHost(host, body), now)
}

func secretPurpose(nonce string, i, n int) string {
	return fmt.Sprintf("secret:%s:%d/%d", nonce, i, n)
}

// SealSecret seals plaintext to the machine's key in parts of at most MaxSealed, each bound to the nonce of the request
// it answers and to its place, so an answer cannot be replayed to another request nor its parts reordered.
func SealSecret(encPub []byte, machineID, nonce string, plaintext []byte) ([]Envelope, error) {
	n := (len(plaintext) + MaxSealed - 1) / MaxSealed
	if n == 0 {
		n = 1
	}
	if n > maxSealedParts {
		return nil, errors.New("too large to seal")
	}
	out := make([]Envelope, 0, n)
	for i := range n {
		part := plaintext[i*MaxSealed : min(len(plaintext), (i+1)*MaxSealed)]
		e, err := Seal(encPub, machineID, secretPurpose(nonce, i, n), part)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// OpenSecret opens what SealSecret sealed for the request with this nonce.
func OpenSecret(encPriv []byte, machineID, nonce string, parts []Envelope) ([]byte, error) {
	n := len(parts)
	if n == 0 || n > maxSealedParts {
		return nil, ErrEnvelope
	}
	var out []byte
	for i, e := range parts {
		p, err := Open(encPriv, machineID, secretPurpose(nonce, i, n), e)
		if err != nil {
			return nil, err
		}
		out = append(out, p...)
	}
	return out, nil
}
