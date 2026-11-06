package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

const safeTimeout = 20 * time.Second

// SafeRoute is the loopback route the runtime asks the hub's safe through.
const SafeRoute = "/secrets/hub"

// SafeAnswer is what the loopback route gives the runtime: the names, or the data read, opened here.
type SafeAnswer struct {
	Names []string          `json:"names,omitempty"`
	Data  map[string]string `json:"data,omitempty"`
}

// refusalFrom is the refusal a hub's error code carries back (safe_<reason>), or unavailable when it says none.
func refusalFrom(status int, raw []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	if reason, ok := strings.CutPrefix(e.Error, "safe_"); ok && reason != "" {
		return &secrets.Refusal{Reason: reason, Err: fmt.Errorf("the hub's safe refused: %s", reason)}
	}
	switch {
	case e.Error == "not_enabled":
		return &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: errors.New("the hub's safe is not switched on for this machine")}
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusLocked:
		return &secrets.Refusal{Reason: secrets.ReasonDenied, Err: fmt.Errorf("the hub did not accept this machine: HTTP %d", status)}
	}
	return &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: fmt.Errorf("the hub's safe answered HTTP %d", status)}
}

// RequestSafe sends one request to the hub's safe (POST /agent/secrets on the host the dial URL points at), signed with
// the machine's key over the body, and opens what comes back sealed. Every error is a secrets.Refusal.
func RequestSafe(ctx context.Context, m *MachineFile, client *http.Client, now time.Time, req hubproto.SecretsRequest) (SafeAnswer, error) {
	unavailable := func(err error) (SafeAnswer, error) {
		return SafeAnswer{}, &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: err}
	}
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		return unavailable(err)
	}
	u, err := url.Parse(m.DialURL)
	if err != nil {
		return unavailable(err)
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	u.Path, u.RawQuery = hubproto.SecretsPath, ""
	body, err := json.Marshal(req)
	if err != nil {
		return unavailable(err)
	}
	auth, err := hubproto.SignSecrets(m.SigningKey(), m.MachineID, now, host, body)
	if err != nil {
		return unavailable(err)
	}
	signed, _ := hubproto.ParseDialHeader(auth)
	ctx, cancel := context.WithTimeout(ctx, safeTimeout)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return unavailable(err)
	}
	hreq.Header.Set("Authorization", auth)
	hreq.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = &http.Client{Timeout: safeTimeout}
	}
	res, err := client.Do(hreq)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return unavailable(fmt.Errorf("could not reach the hub at %s: %w", m.HubURL, err))
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return SafeAnswer{}, refusalFrom(res.StatusCode, raw)
	}
	var ans hubproto.SecretsAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return unavailable(errors.New("the hub gave an answer that is not the safe's"))
	}
	out := SafeAnswer{Names: ans.Names}
	if len(ans.Sealed) > 0 {
		plain, err := hubproto.OpenSecret(m.SealingKey(), m.MachineID, signed.Nonce, ans.Sealed)
		if err != nil {
			return SafeAnswer{}, &secrets.Refusal{Reason: secrets.ReasonFailed, Err: errors.New("the hub's answer does not open with this machine's key")}
		}
		if err := json.Unmarshal(plain, &out.Data); err != nil {
			return SafeAnswer{}, &secrets.Refusal{Reason: secrets.ReasonFailed, Err: errors.New("the hub sealed something that is not the safe's data")}
		}
	}
	return out, nil
}

// safeRoute is POST /secrets/hub on the loopback: the runtime of this machine asks the hub's safe. Only a hub machine
// has a safe there; a refusal comes back with its reason, which the runtime shows as its own.
func (a *Agent) safeRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	m := a.safeMachine()
	if m == nil {
		reply(w, 409, map[string]string{"error": "this machine has no hub that keeps its safe", "reason": secrets.ReasonUnavailable})
		return
	}
	var req hubproto.SecretsRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, hubproto.MaxSecretsBody)).Decode(&req); err != nil {
		reply(w, 400, map[string]string{"error": "not a request of the safe", "reason": secrets.ReasonFailed})
		return
	}
	ans, err := RequestSafe(r.Context(), m, nil, time.Now(), req)
	if err != nil {
		reply(w, 502, map[string]string{"error": err.Error(), "reason": secrets.ReasonOf(err)})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, 200, ans)
}

// safeMachine is the machine the hub's safe knows this agent by: its own hub machine, or the machine of the observe link
// when the operator opted in with AGENT_HUB_SAFE=on (the closed cluster's machine keeps its secrets at home otherwise).
// nil: there is no hub to keep the safe.
func (a *Agent) safeMachine() *MachineFile {
	switch {
	case a.cfg.Machine != nil:
		return a.cfg.Machine
	case a.cfg.ObserveMachine != nil && a.cfg.ObserveSafe:
		return a.cfg.ObserveMachine
	}
	return nil
}

// SafeOwnFolder is the folder LocalSafe shows the owner's secrets in: secrets.NewRemote(LocalSafe{…}, SafeOwnFolder, dir).
// The hub puts them where it chooses; any other path is a reference.
const SafeOwnFolder = "me"

// LocalSafe is the hub's safe as the runtime sees it, through the machine agent whose agent.json lies in DataDir: a
// secrets.KV whose folder SafeOwnFolder is the owner's secrets, and every other path a reference.
type LocalSafe struct {
	DataDir, TokenHeader string
	HTTP                 *http.Client
}

var _ secrets.KV = LocalSafe{}

func (l LocalSafe) call(req hubproto.SecretsRequest) (SafeAnswer, error) {
	status, raw, err := CallLocal(l.HTTP, l.DataDir, l.TokenHeader, SafeRoute, req, 2*safeTimeout)
	if err != nil {
		return SafeAnswer{}, &secrets.Refusal{Reason: secrets.ReasonUnavailable, Err: fmt.Errorf("the machine agent: %w", err)}
	}
	if status != http.StatusOK {
		var e struct{ Error, Reason string }
		_ = json.Unmarshal(raw, &e)
		if e.Reason == "" {
			e.Reason = secrets.ReasonUnavailable
		}
		if e.Error == "" {
			e.Error = fmt.Sprintf("the machine agent answered %d", status)
		}
		return SafeAnswer{}, &secrets.Refusal{Reason: e.Reason, Err: errors.New(e.Error)}
	}
	var ans SafeAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		return SafeAnswer{}, &secrets.Refusal{Reason: secrets.ReasonFailed, Err: errors.New("the machine agent gave an answer that is not the safe's")}
	}
	return ans, nil
}

// own is the name of the owner's secret at path, when path is one.
func own(path string) (string, bool) {
	name, ok := strings.CutPrefix(path, SafeOwnFolder+"/")
	return name, ok && !strings.Contains(name, "/")
}

func notOwn() error {
	return &secrets.Refusal{Reason: secrets.ReasonDenied, Err: errors.New("the hub's safe changes the owner's secrets only")}
}

// Read reads a secret of the owner or a reference.
func (l LocalSafe) Read(_ context.Context, path string) (map[string]string, error) {
	req := hubproto.SecretsRequest{Op: hubproto.SecretsRef, Path: path}
	if name, ok := own(path); ok {
		req = hubproto.SecretsRequest{Op: hubproto.SecretsRead, Name: name}
	}
	ans, err := l.call(req)
	return ans.Data, err
}

func (l LocalSafe) Write(_ context.Context, path string, data map[string]string) error {
	name, ok := own(path)
	if !ok {
		return notOwn()
	}
	_, err := l.call(hubproto.SecretsRequest{Op: hubproto.SecretsWrite, Name: name, Data: data})
	return err
}

func (l LocalSafe) Delete(_ context.Context, path string) error {
	name, ok := own(path)
	if !ok {
		return notOwn()
	}
	_, err := l.call(hubproto.SecretsRequest{Op: hubproto.SecretsDelete, Name: name})
	return err
}

// List lists the owner's secrets; the hub keeps them in a folder of its choice, so only that folder lists.
func (l LocalSafe) List(_ context.Context, path string) ([]string, error) {
	if strings.Trim(path, "/") != SafeOwnFolder {
		return nil, notOwn()
	}
	ans, err := l.call(hubproto.SecretsRequest{Op: hubproto.SecretsList})
	return ans.Names, err
}
