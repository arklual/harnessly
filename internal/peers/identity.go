package peers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// Sender is who a message is from, as this runtime knows it for sure.
type Sender struct {
	// Person is the owner's fleet identity (an email), PersonName how people call them.
	Person     string `json:"person"`
	PersonName string `json:"personName"`
	// Machine is the fleet id of the sender's machine, MachineName its name.
	Machine     string `json:"machine"`
	MachineName string `json:"machineName"`
}

// Name is what the card says the sender is called: the person's name, else their identity, else the machine.
func (s Sender) Name() string {
	switch {
	case s.PersonName != "":
		return s.PersonName
	case s.Person != "":
		return strings.SplitN(s.Person, "@", 2)[0]
	}
	return s.MachineLabel()
}

// MachineLabel is the machine's name, or its id.
func (s Sender) MachineLabel() string {
	if s.MachineName != "" {
		return s.MachineName
	}
	return s.Machine
}

// The audiences of the two tokens of a message between machines. Both are HS256 tokens signed with a runtime's own
// secret — the one the fleet's gateway already signs that runtime's page sessions with — so no new secret exists:
//
//   - RelayAudience: the sending runtime signs its request to the gateway with its own secret. The gateway knows every
//     runtime's secret and checks it under the machine the token claims, so a runtime can speak only as itself.
//   - InboxAudience: the gateway signs the forwarded message with the receiving runtime's secret, naming the sender it
//     checked. The receiving runtime verifies it with its own secret (VerifyInbox): only the gateway, or the runtime
//     itself, can make one.
//
// A token with an audience is never a page session (agent.SessionSubject refuses it), and a page session is never one
// of these (VerifiedAudience requires the audience).
const (
	RelayAudience = "harnessly-peer-relay"
	InboxAudience = "harnessly-peer"
	// TokenTTL: a token is made for one request.
	TokenTTL = 60 * time.Second
)

var machineID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func claim(c map[string]any, k string) string {
	s, _ := c[k].(string)
	return strings.TrimSpace(s)
}

// ErrUnverified: the token is missing, forged, expired, for another audience or names no machine.
var ErrUnverified = errors.New("unverified sender")

func senderOf(c map[string]any) (Sender, error) {
	s := Sender{Person: claim(c, "sub"), PersonName: claim(c, "name"), Machine: claim(c, "machine"),
		MachineName: claim(c, "machineName")}
	if !machineID.MatchString(s.Machine) || s.Person == "" {
		return Sender{}, ErrUnverified
	}
	return s, nil
}

func sign(secret []byte, aud string, s Sender) string {
	return agent.SignPayload(secret, aud, map[string]any{"sub": s.Person, "name": s.PersonName, "machine": s.Machine,
		"machineName": s.MachineName}, TokenTTL)
}

// SignInbox is the token the gateway puts on a message it forwards to the runtime whose secret this is (the runtime's
// tests sign with it too).
func SignInbox(secret []byte, s Sender) string { return sign(secret, InboxAudience, s) }

// VerifyInbox is the sender a forwarded message's token names, if this runtime's secret signed it for the inbox.
func VerifyInbox(secret []byte, token string, now time.Time) (Sender, error) {
	c, ok := agent.VerifiedAudience(secret, token, InboxAudience, now)
	if !ok {
		return Sender{}, ErrUnverified
	}
	return senderOf(c)
}

// SignRelay is this runtime's request to the gateway, signed with its own secret, speaking as itself.
func SignRelay(secret []byte, self Sender) string { return sign(secret, RelayAudience, self) }

// VerifyRelay is what the gateway does with a relay request (here for the contract's tests and a gateway in Go): the
// token checked with the secret of the machine it claims. The claim only picks the secret; the signature proves it.
func VerifyRelay(secretOf func(machine string) []byte, token string, now time.Time) (Sender, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Sender{}, ErrUnverified
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Sender{}, ErrUnverified
	}
	var unverified map[string]any
	if json.Unmarshal(raw, &unverified) != nil {
		return Sender{}, ErrUnverified
	}
	secret := secretOf(claim(unverified, "machine"))
	if len(secret) == 0 {
		return Sender{}, ErrUnverified
	}
	c, ok := agent.VerifiedAudience(secret, token, RelayAudience, now)
	if !ok {
		return Sender{}, ErrUnverified
	}
	return senderOf(c)
}
