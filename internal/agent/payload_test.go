package agent

import (
	"testing"
	"time"
)

// Any token signed with the pair's secret names its person, a page session or not; another secret names nobody.
func TestVerifiedPayload(t *testing.T) {
	secret := []byte("pair-secret")
	now := time.Now()
	for _, aud := range []string{"", "agent"} {
		p, ok := VerifiedPayload(secret, SignHS256(secret, "person@example.com", aud, time.Hour), now)
		if !ok || p["sub"] != "person@example.com" {
			t.Errorf("aud %q: %v %v", aud, ok, p)
		}
	}
	if _, ok := VerifiedPayload([]byte("other"), SignHS256(secret, "x", "", time.Hour), now); ok {
		t.Error("a token of another secret")
	}
	if _, ok := VerifiedPayload(secret, SignHS256(secret, "x", "", time.Hour), now.Add(2*time.Hour)); ok {
		t.Error("an expired token")
	}
}
