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
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const sitesSessionTimeout = 20 * time.Second

// SitesSession is the hub's answer to a machine that asked for a publishing session: where the sites API is, the
// space the machine belongs to and the token to publish with.
type SitesSession struct {
	URL       string `json:"url"`
	Space     string `json:"space"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

// RequestSitesSession asks the hub (POST /agent/sites on the host the dial URL points at) for a publishing session of
// the machine's space, signed with the machine's own key.
func RequestSitesSession(ctx context.Context, m *MachineFile, client *http.Client, now time.Time, person string) (SitesSession, error) {
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		return SitesSession{}, err
	}
	u, err := url.Parse(m.DialURL)
	if err != nil {
		return SitesSession{}, err
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	u.Path, u.RawQuery = "/agent/sites", ""
	auth, err := hubproto.SignSites(m.SigningKey(), m.MachineID, now, host)
	if err != nil {
		return SitesSession{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, sitesSessionTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"person": person})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return SitesSession{}, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = &http.Client{Timeout: sitesSessionTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return SitesSession{}, fmt.Errorf("could not reach the hub at %s: %w", m.HubURL, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	switch res.StatusCode {
	case http.StatusOK:
		var out SitesSession
		if json.Unmarshal(raw, &out) != nil || out.Token == "" || out.Space == "" {
			return SitesSession{}, errors.New("the hub gave an answer that is not a publishing session")
		}
		return out, nil
	case http.StatusNotFound:
		return SitesSession{}, errors.New("this hub does not publish sites")
	case http.StatusUnauthorized, http.StatusForbidden:
		return SitesSession{}, errors.New("the hub did not accept this machine: check the clock, or the machine is not in this hub")
	}
	return SitesSession{}, fmt.Errorf("the hub did not give a session: HTTP %d", res.StatusCode)
}

// sitesSessionRoute is POST /sites/session on the loopback: the runtime of this machine asks for the token it publishes
// a site with. The machine is the agent's, not the runtime's, so the signature is made here.
func (a *Agent) sitesSessionRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	if a.cfg.Machine == nil {
		reply(w, 409, map[string]string{"error": "this machine is not in a hub", "code": "no_hub"})
		return
	}
	var in struct {
		Person string `json:"person"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in)
	s, err := RequestSitesSession(r.Context(), a.cfg.Machine, nil, time.Now(), in.Person)
	if err != nil {
		reply(w, 502, map[string]string{"error": err.Error(), "code": "hub"})
		return
	}
	reply(w, 200, s)
}

// FetchSitesSession is what a runtime does to publish: it asks the machine agent whose agent.json lies in dataDir for a
// session. The error says what to tell the person (no agent, no hub, a hub that does not publish).
func FetchSitesSession(ctx context.Context, dataDir, tokenHeader, person string) (SitesSession, error) {
	port, token, ok := localAgent(dataDir)
	if !ok {
		return SitesSession{}, errors.New("no machine agent runs here")
	}
	if tokenHeader == "" {
		tokenHeader = "x-agent-local-token"
	}
	body, _ := json.Marshal(map[string]string{"person": person})
	ctx, cancel := context.WithTimeout(ctx, 2*sitesSessionTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/sites/session", bytes.NewReader(body))
	if err != nil {
		return SitesSession{}, err
	}
	req.Header.Set(tokenHeader, token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return SitesSession{}, fmt.Errorf("the machine agent does not answer: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode != http.StatusOK {
		var e struct{ Error string }
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = fmt.Sprintf("the machine agent answered %d", res.StatusCode)
		}
		return SitesSession{}, errors.New(e.Error)
	}
	var out SitesSession
	if err := json.Unmarshal(raw, &out); err != nil {
		return SitesSession{}, err
	}
	return out, nil
}
