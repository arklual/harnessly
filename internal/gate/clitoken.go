package gate

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CLIToken checks the runtime's cli-token, kept in cli-token in the data folder: the CLIs and the agent hooks carry it.
//
// It also checks the agent tokens it issues (IssueAgent): `agent.<session>.<expMs>.<hmac>`, the HMAC-SHA256 of
// "agent|<session>|<expMs>" under a key of its own in the data folder (agent-token-key). An agent token is the token of
// one launch of an agent; the routes that change the apps refuse it (AgentOf), whatever the agent's tools are allowed.
type CLIToken struct {
	DataDir string
	// Headers carry the cli-token; a request is let in by the first one it has.
	Headers []string
	// Now is the clock agent tokens expire by; nil is the machine's.
	Now func() time.Time

	keyMu sync.Mutex
}

// AgentTokenFile is the file the agent tokens' key is kept in, in the data folder.
const AgentTokenFile = "agent-token-key"

// agentSessionRe: a session names the launch in the token, so it stays plain and has no dot.
var agentSessionRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrAgentSession: the session of an agent token is not one agentSessionRe takes.
var ErrAgentSession = errors.New("invalid agent token session")

func (t *CLIToken) token() string {
	raw, _ := os.ReadFile(filepath.Join(t.DataDir, "cli-token"))
	return strings.TrimSpace(string(raw))
}

func (t *CLIToken) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// agentKey is the key agent tokens are signed with; create makes one when there is none.
func (t *CLIToken) agentKey(create bool) ([]byte, error) {
	path := filepath.Join(t.DataDir, AgentTokenFile)
	if raw, err := os.ReadFile(path); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(key) < 32 {
			return nil, errors.New(AgentTokenFile + " is not a key")
		}
		return key, nil
	} else if !create || !os.IsNotExist(err) {
		return nil, err
	}
	t.keyMu.Lock()
	defer t.keyMu.Unlock()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		// Another launch made it first: that one is the key.
		return t.agentKey(false)
	}
	if err != nil {
		return nil, err
	}
	_, werr := f.WriteString(hex.EncodeToString(key) + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path)
		return nil, werr
	}
	return key, nil
}

func agentMAC(key []byte, session, exp string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("agent|" + session + "|" + exp))
	return hex.EncodeToString(m.Sum(nil))
}

// IssueAgent is an agent token for a session that holds for ttl.
func (t *CLIToken) IssueAgent(session string, ttl time.Duration) (string, error) {
	if !agentSessionRe.MatchString(session) {
		return "", ErrAgentSession
	}
	key, err := t.agentKey(true)
	if err != nil {
		return "", err
	}
	exp := strconv.FormatInt(t.now().Add(ttl).UnixMilli(), 10)
	return "agent." + session + "." + exp + "." + agentMAC(key, session, exp), nil
}

// agentSession is the session of a valid, unexpired agent token; false for anything else.
func (t *CLIToken) agentSession(v string) (string, bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 4 || parts[0] != "agent" || !agentSessionRe.MatchString(parts[1]) {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || t.now().UnixMilli() >= exp {
		return "", false
	}
	key, err := t.agentKey(false)
	if err != nil {
		return "", false
	}
	if !hmac.Equal([]byte(parts[3]), []byte(agentMAC(key, parts[1], parts[2]))) {
		return "", false
	}
	return parts[1], true
}

// Principal is who a request with a token is: the person's CLI (the cli-token), or an agent's launch.
type Principal struct {
	Agent   bool
	Session string
}

// Accepts is the principal of one token value: the cli-token or a valid agent token; false for anything else.
func (t *CLIToken) Accepts(v string) (Principal, bool) {
	if v == "" {
		return Principal{}, false
	}
	if token := t.token(); token != "" && subtle.ConstantTimeCompare([]byte(v), []byte(token)) == 1 {
		return Principal{}, true
	}
	if strings.HasPrefix(v, "agent.") {
		if session, ok := t.agentSession(v); ok {
			return Principal{Agent: true, Session: session}, true
		}
	}
	return Principal{}, false
}

// PrincipalOf is the principal of the first header with a token that holds; false when none does.
func (t *CLIToken) PrincipalOf(r *http.Request) (Principal, bool) {
	for _, h := range t.Headers {
		if p, ok := t.Accepts(r.Header.Get(h)); ok {
			return p, true
		}
	}
	return Principal{}, false
}

// OK checks a request to the internal routes: the runtime's cli-token, or an agent token of its own, in one of the
// headers.
func (t *CLIToken) OK(r *http.Request) bool {
	_, ok := t.PrincipalOf(r)
	return ok
}

// AgentOf says whether the request is an agent's: a valid agent token, not the person's cli-token.
func (t *CLIToken) AgentOf(r *http.Request) (session string, agent bool) {
	if t == nil {
		return "", false
	}
	p, ok := t.PrincipalOf(r)
	return p.Session, ok && p.Agent
}
