package hubproto

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestReregisterProofIsBoundToTheKeyTheCodeAndTheMachine(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	c := SignReregister(priv, "machine-1", "hx_code", "newkey")
	if !VerifyReregister(pub, c, "hx_code", "newkey") {
		t.Fatal("the machine's own proof is refused")
	}
	if VerifyReregister(otherPub, c, "hx_code", "newkey") {
		t.Error("a proof passes under another machine's key")
	}
	if VerifyReregister(pub, c, "hx_other", "newkey") {
		t.Error("a proof passes with another code")
	}
	if VerifyReregister(pub, c, "hx_code", "anotherkey") {
		t.Error("a proof passes for another new key")
	}
	if VerifyReregister(pub, &Reregister{MachineID: "machine-2", Proof: c.Proof}, "hx_code", "newkey") {
		t.Error("a proof passes for another machine id")
	}
	if VerifyReregister(pub, SignReregister(otherPriv, "machine-1", "hx_code", "newkey"), "hx_code", "newkey") {
		t.Error("a proof signed by a stranger's key passes")
	}
	if VerifyReregister(pub, &Reregister{MachineID: "machine-1", Proof: "!"}, "hx_code", "newkey") || VerifyReregister(pub, nil, "hx_code", "newkey") {
		t.Error("a damaged or missing proof passes")
	}
}
