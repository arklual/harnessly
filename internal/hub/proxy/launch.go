package proxy

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Starting a conversation from outside. A service that holds the launch token (HUB_LAUNCH_TOKEN) asks the hub
// to start a conversation for a person on one of their own machines: a bot that turns a chat message into a task is the
// first caller. The hub does not take anyone's word for a machine: the service names the person, and the conversation
// starts only on a machine that person connected, once the rollout switch is on for them.
//
//	POST /api/platform/launch   Authorization: Bearer <HUB_LAUNCH_TOKEN>
//	{"person": "<email>", "prompt": "…", "provider": "claude|codex", "machineId": "…", "workspaceId": "…"}
//
// 201 carries what the runtime answered plus machineId, machineName and url (the conversation in the workspace). An
// error carries `retryable: true` when nothing reached a machine, so the caller may ask again later.

// launchSwitch is the rollout switch that lets the launch token reach a person's machines (per person and machine; no
// row means off). docs/hub/rollout-switches.md.
const launchSwitch = "prompt-launch"

const (
	launchPath    = "/api/platform/launch"
	launchWait    = 60 * time.Second
	maxLaunchBody = 256 << 10
	maxLaunchText = 100_000
	minLaunchKey  = 32
)

type launchBody struct {
	Person      string  `json:"person"`
	Prompt      string  `json:"prompt"`
	Provider    string  `json:"provider"`
	MachineID   string  `json:"machineId"`
	WorkspaceID *string `json:"workspaceId"`
}

// launchErr is an error answer of the launch route.
type launchErr struct {
	status    int
	code, msg string
	retryable bool
}

func (e *launchErr) Error() string { return e.code + ": " + e.msg }

func (s *Service) mountLaunch(mux *http.ServeMux) {
	mux.HandleFunc("POST "+launchPath, func(w http.ResponseWriter, r *http.Request) {
		err := s.launch(w, r)
		if err == nil {
			return
		}
		var le *launchErr
		if errors.As(err, &le) {
			body := map[string]any{"error": le.code, "message": le.msg}
			if le.retryable {
				body["retryable"] = true
			}
			httpx.WriteJSON(w, le.status, body)
			return
		}
		httpx.WriteError(w, r, err)
	})
}

// launch starts the conversation. A missing or short HUB_LAUNCH_TOKEN shuts the route (404, as if there were none).
func (s *Service) launch(w http.ResponseWriter, r *http.Request) error {
	key := s.cfg.LaunchToken
	if len(key) < minLaunchKey {
		return httpx.ErrNotFound
	}
	given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(given), []byte(key)) != 1 {
		return httpx.ErrForbidden
	}
	var b launchBody
	r.Body = http.MaxBytesReader(w, r.Body, maxLaunchBody)
	if err := httpx.Decode(r, &b); err != nil {
		return err
	}
	b.Person = strings.ToLower(strings.TrimSpace(b.Person))
	if b.Provider == "" {
		b.Provider = "claude"
	}
	if b.Person == "" || strings.TrimSpace(b.Prompt) == "" || len(b.Prompt) > maxLaunchText ||
		(b.Provider != "claude" && b.Provider != "codex") || (b.MachineID != "" && !machineID.MatchString(b.MachineID)) {
		return &launchErr{status: http.StatusBadRequest, code: "invalid_request", msg: "Нужны person, prompt и provider claude или codex"}
	}
	u, err := s.db.S().UserByEmail(r.Context(), b.Person)
	if errors.Is(err, store.ErrNotFound) || (err == nil && u.DisabledAt != nil) {
		return &launchErr{status: http.StatusNotFound, code: "unknown_person", msg: "Нет такого человека"}
	}
	if err != nil {
		return err
	}
	id, err := s.launchMachine(r.Context(), u, b.MachineID)
	if err != nil {
		return err
	}
	m, err := s.admit(r.Context(), u.ID, id)
	if err == nil {
		err = s.trustedOnHub(r.Context(), m)
	}
	var he *httpx.Error
	if errors.As(err, &he) && (he.Status == http.StatusNotFound || he.Status == http.StatusForbidden) {
		return &launchErr{status: http.StatusNotFound, code: "unknown_machine", msg: "У человека нет такой машины"}
	}
	if err != nil {
		return err
	}
	if s.switches == nil || !s.switches.On(r.Context(), launchSwitch, strings.ToLower(u.Email), m.ID) {
		return &launchErr{status: http.StatusServiceUnavailable, code: "launch_off", msg: "Запуск через хаб для этого человека не включён"}
	}
	value, err := s.launchStart(r, u, m, b)
	if err != nil {
		return err
	}
	value["machineId"] = m.ID
	value["machineName"] = m.Name
	if conv, _ := value["conversationId"].(string); conv != "" {
		value["url"] = strings.TrimRight(s.cfg.PublicURL, "/") + workspaceBase + "/?c=" + url.QueryEscape("agent:"+m.ID+":"+conv)
	}
	httpx.WriteJSON(w, http.StatusCreated, value)
	return nil
}

// launchMachine is the machine the conversation starts on: the named one if the person connected it, else the first
// of their machines that is connected now. A machine the person did not connect is never chosen, named or not.
func (s *Service) launchMachine(ctx context.Context, u store.User, named string) (string, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM machines WHERE COALESCE(NULLIF(connected_by, ''), created_by) = ? AND revoked_at IS NULL AND disabled_at IS NULL ORDER BY created_at, id`, u.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	first, own := "", false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		if named != "" {
			own = own || id == named
		} else if first == "" && s.reg.Get(id) != nil {
			first = id
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if named != "" {
		if !own {
			return "", &launchErr{status: http.StatusNotFound, code: "unknown_machine", msg: "У человека нет такой машины"}
		}
		if s.reg.Get(named) == nil {
			return "", &launchErr{status: http.StatusBadGateway, code: "machine_offline", msg: "Машина не на связи", retryable: true}
		}
		return named, nil
	}
	if first == "" {
		return "", &launchErr{status: http.StatusBadGateway, code: "machine_offline", msg: "Ни одна машина человека не на связи", retryable: true}
	}
	return first, nil
}

// launchStart asks the machine's runtime to start the conversation as the person, over the session the workspace page
// uses. The runtime's prefix of the platform API is read from the machine's own page.
func (s *Service) launchStart(r *http.Request, u store.User, m store.Machine, b launchBody) (map[string]any, error) {
	payload := map[string]any{"provider": b.Provider, "prompt": b.Prompt}
	if b.WorkspaceID != nil {
		payload["workspaceId"] = *b.WorkspaceID
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	prefix := "/api/platform"
	if got, ok := s.machineSettings(r, u, m); ok && got.platformAPI != "" {
		prefix = got.platformAPI
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), launchWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, workspaceBase+prefix+"/conversations", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if pu, err := url.Parse(s.cfg.PublicURL); err == nil {
		req.Host = pu.Host
	}
	req.RemoteAddr = "127.0.0.1:0"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Harnessly-Device", "hub-launch")
	rec := &pageRecorder{header: http.Header{}}
	s.forward(rec, req, u, m)
	if !rec.wrote {
		return nil, &launchErr{status: http.StatusBadGateway, code: "machine_error", msg: "Машина не ответила"}
	}
	if rec.status < 200 || rec.status >= 300 {
		var ans struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.body.Bytes(), &ans)
		detail := ans.Error
		if detail == "" {
			detail = http.StatusText(rec.status)
		}
		// The link dropped before the request was carried: nothing was started, so the caller may try again.
		never := rec.status == http.StatusBadGateway && ans.Error == "machine_offline"
		return nil, &launchErr{status: http.StatusBadGateway, code: "runtime_error", msg: "runtime answered " + detail, retryable: never}
	}
	var value map[string]any
	if json.Unmarshal(rec.body.Bytes(), &value) != nil || value == nil {
		return nil, &launchErr{status: http.StatusBadGateway, code: "runtime_error", msg: "runtime answered unreadable"}
	}
	return value, nil
}
