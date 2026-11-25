package proxy

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const inboxTriagePath = "/api/platform" + hubproto.InboxTriageSuffix
const inboxTriageSwitch = "inbox-triage"

// This endpoint has neither a caller-selected model nor a general machine proxy. The launch credential is not accepted.
func (s *Service) inboxTriage(w http.ResponseWriter, r *http.Request) error {
	key := s.cfg.InboxTriageToken
	if len(key) < 32 || key == s.cfg.LaunchToken {
		return httpx.ErrNotFound
	}
	bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(bearer), []byte(key)) != 1 {
		return httpx.ErrForbidden
	}
	var b struct {
		hubproto.InboxTriageInput
		Person    string `json:"person"`
		MachineID string `json:"machineId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, hubproto.InboxTriageMaxBody)
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	b.Person = strings.ToLower(strings.TrimSpace(b.Person))
	if !b.InboxTriageInput.Valid() || b.Person == "" || len(b.Person) > 254 || !machineID.MatchString(b.MachineID) {
		return httpx.New(http.StatusBadRequest, "invalid_request", "Invalid Inbox triage input")
	}
	u, err := s.db.S().UserByEmail(r.Context(), b.Person)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.DisabledAt != nil) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	id, err := s.launchMachine(r.Context(), u, b.MachineID)
	if err != nil {
		var le *launchErr
		if errors.As(err, &le) {
			return httpx.New(le.status, le.code, le.msg)
		}
		return err
	}
	m, err := s.admit(r.Context(), u.ID, id)
	if err != nil {
		return err
	}
	if err := s.trustedOnHub(r.Context(), m); err != nil {
		return err
	}
	if s.switches == nil || !s.switches.On(r.Context(), inboxTriageSwitch, b.Person, id) {
		return httpx.New(http.StatusServiceUnavailable, "inbox_triage_off", "Inbox triage is not enabled on this machine")
	}
	leave, ok := s.enter(u.ID, id)
	if !ok {
		return httpx.New(http.StatusTooManyRequests, "machine_busy", "Machine is busy")
	}
	defer leave()
	ctx, cancel := context.WithTimeout(r.Context(), 80*time.Second)
	defer cancel()
	defer s.track(u.ID+"/"+m.SpaceID, cancel)()
	unavailable := httpx.New(http.StatusBadGateway, "triage_runtime_unavailable", "Inbox triage runtime unavailable")
	var g hubproto.SessionGrant
	if err := s.reg.DoInto(ctx, id, hubproto.OpInboxTriageGrant, map[string]string{"subject": u.Email}, &g, grantTimeout); err != nil || g.Token == "" || g.CookieName == "" {
		return unavailable
	}
	prefix := "/api/platform"
	if got, ok := s.machineSettings(r.WithContext(ctx), u, m); ok && got.platformAPI != "" {
		prefix = got.platformAPI
	}
	raw, _ := json.Marshal(b.InboxTriageInput)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://runtime"+prefix+hubproto.InboxTriageSuffix, bytes.NewReader(raw))
	if err != nil {
		return unavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", runtimeCookie(g))
	res, err := s.transport(id).RoundTrip(req)
	if err != nil {
		return unavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return unavailable
	}
	var out hubproto.InboxTriageOutput
	var result struct {
		Used  *bool  `json:"used"`
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	reply, err := io.ReadAll(io.LimitReader(res.Body, hubproto.InboxTriageMaxReply+1))
	if err != nil || len(reply) > hubproto.InboxTriageMaxReply || json.Unmarshal(reply, &result) != nil || result.Used == nil {
		return unavailable
	}
	out.Used = *result.Used
	if result.Error != "" {
		return httpx.New(http.StatusBadGateway, "triage_model_failed", "Selected Inbox model failed; see machine settings")
	}
	if out.Used {
		out.Text = result.Text
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
