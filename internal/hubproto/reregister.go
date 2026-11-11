package hubproto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Reregister is a machine's claim to the id it was registered under before, in RegisterRequest.Previous. Proof is made
// with the signing key of that registration, which only the machine's data folder holds, and covers the one-time code
// and the new public key: it cannot be made by somebody who only knows the machine's id, and it cannot be replayed
// with another code or for another key.
type Reregister struct {
	MachineID string `json:"machineId"`
	// Proof is the Ed25519 signature, base64 std.
	Proof string `json:"proof"`
}

// reregisterMessage is what Proof signs. The code goes in as its hash: the signature travels beside the code, and
// nothing that names the code should be worth keeping.
func reregisterMessage(machineID, code, newPublicKey string) []byte {
	sum := sha256.Sum256([]byte(code))
	return []byte("harnessly/reregister/v1\n" + machineID + "\n" + hex.EncodeToString(sum[:]) + "\n" + newPublicKey)
}

// SignReregister makes the claim with the signing key of the registration that is being replaced. newPublicKey is
// RegisterRequest.PublicKey as it is sent.
func SignReregister(prev ed25519.PrivateKey, machineID, code, newPublicKey string) *Reregister {
	sig := ed25519.Sign(prev, reregisterMessage(machineID, code, newPublicKey))
	return &Reregister{MachineID: machineID, Proof: base64.StdEncoding.EncodeToString(sig)}
}

// VerifyReregister says whether the claim was signed by the key the hub holds for the machine it names.
func VerifyReregister(held ed25519.PublicKey, c *Reregister, code, newPublicKey string) bool {
	if c == nil || len(held) != ed25519.PublicKeySize {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(c.Proof)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(held, reregisterMessage(c.MachineID, code, newPublicKey), sig)
}
