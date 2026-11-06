package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A chat moves between two machines of one person (flag runtime.fleet). The move itself is the runtime's own
// (internal/chatmove: describe, preflight, export, import, hold, seal); the hub is the cluster's gateway for
// it. It checks the person's access to both machines, makes a one-time key for the operation (the machines share no
// secret, and the hub does not read the bundle), and drives the steps through the agent tunnels. The answers have the
// shape the page of the chat already reads (web/app/next/features/chat-move).

const (
	moveStepTimeout = 130 * time.Second
	moveProbeTime   = 15 * time.Second
	moveMaxAnswer   = 64 << 20
)

var movePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// moveFail is a refusal in the words the person reads: {error, code} is what the page's MoveError takes.
type moveFail struct {
	Status int
	Code   string
	Text   string
	Checks any
}

func (f *moveFail) Error() string { return f.Code }

func writeMoveFail(w http.ResponseWriter, f *moveFail) {
	body := map[string]any{"error": f.Text, "code": f.Code}
	if f.Checks != nil {
		body["checks"] = f.Checks
	}
	httpx.WriteJSON(w, f.Status, body)
}

var (
	failOff     = &moveFail{Status: http.StatusNotFound, Code: "not_enabled", Text: "Перенос чатов между машинами не включён"}
	failNoSrc   = &moveFail{Status: http.StatusNotFound, Code: "not_found", Text: "Чат или машина не найдены"}
	failBadReq  = &moveFail{Status: http.StatusBadRequest, Code: "bad_request", Text: "Неверный запрос на перенос"}
	failSameVM  = &moveFail{Status: http.StatusBadRequest, Code: "same_machine", Text: "Чат уже на этой машине: выберите другую"}
	failNoRight = &moveFail{Status: http.StatusForbidden, Code: "no_access", Text: "У вас нет доступа к выбранной машине"}
)

// moveSide is a machine of a move: admitted for the person, in an organization that has the flag on.
type moveSide struct {
	m  store.Machine
	sp store.Space
}

func (s *Service) moveAdmit(ctx context.Context, userID, id string, none *moveFail) (moveSide, *moveFail) {
	if !machineID.MatchString(id) {
		return moveSide{}, none
	}
	m, err := s.admit(ctx, userID, id)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) && he.Status >= 500 || !errors.As(err, &he) {
			s.log.Error("chat move: access check failed", "error", err)
			return moveSide{}, &moveFail{Status: http.StatusInternalServerError, Code: "internal", Text: "Не удалось проверить доступ к машине"}
		}
		return moveSide{}, none
	}
	sp, err := s.db.S().SpaceByID(ctx, m.SpaceID)
	if err != nil || !s.fleetOn(ctx, sp) {
		return moveSide{}, none
	}
	return moveSide{m: m, sp: sp}, nil
}

// moveCall is one step of a runtime's move. A refusal of the runtime comes back as it is (its words are the person's);
// a machine that is away or does not answer gets the hub's own.
func (s *Service) moveCall(ctx context.Context, u store.User, m store.Machine, step string, in, out any) *moveFail {
	if s.reg.Get(m.ID) == nil {
		return offlineFail(m)
	}
	g, err := s.grant(ctx, u, m)
	if err != nil {
		return s.moveDown(m, err)
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return failBadReq
	}
	ctx, cancel := context.WithTimeout(ctx, moveStepTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://runtime/api/v2/move/"+step, bytes.NewReader(raw))
	if err != nil {
		return failBadReq
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", runtimeCookie(g))
	res, err := (&http.Client{Transport: s.transport(m.ID), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return s.moveDown(m, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, moveMaxAnswer+1))
	if err != nil || len(body) > moveMaxAnswer {
		return s.moveDown(m, errors.New("answer unreadable"))
	}
	if res.StatusCode == http.StatusUnauthorized {
		s.dropGrant(u.ID, m.ID)
	}
	if res.StatusCode != http.StatusOK {
		var r struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Checks  any    `json:"checks"`
		}
		if json.Unmarshal(body, &r) != nil || r.Code == "" {
			return &moveFail{Status: http.StatusBadGateway, Code: "machine_error",
				Text: "Машина «" + m.Name + "» не умеет переносить чаты: обновите на ней harnessly"}
		}
		return &moveFail{Status: res.StatusCode, Code: r.Code, Text: r.Message, Checks: r.Checks}
	}
	if out != nil && json.Unmarshal(body, out) != nil {
		return s.moveDown(m, errors.New("answer is not json"))
	}
	return nil
}

func offlineFail(m store.Machine) *moveFail {
	return &moveFail{Status: http.StatusConflict, Code: "machine_offline",
		Text: "Машина «" + m.Name + "» сейчас не на связи: включите её и попробуйте ещё раз"}
}

func (s *Service) moveDown(m store.Machine, err error) *moveFail {
	if errors.Is(err, errOffline) || s.reg.Get(m.ID) == nil {
		return offlineFail(m)
	}
	s.log.Info("chat move: machine did not answer", "machine", m.ID, "error", err)
	return &moveFail{Status: http.StatusBadGateway, Code: "machine_error", Text: "Машина «" + m.Name + "» не отвечает: попробуйте ещё раз через минуту"}
}

// serveMove answers /api/v2/chat-move/… for a person already signed in.
func (s *Service) serveMove(w http.ResponseWriter, r *http.Request, u store.User) {
	switch path := strings.TrimSuffix(r.URL.Path, "/"); {
	case path == "/api/v2/chat-move/status" && r.Method == http.MethodGet:
		s.moveStatus(w, r, u)
	case path == "/api/v2/chat-move/targets" && r.Method == http.MethodGet:
		s.moveTargets(w, r, u)
	case path == "/api/v2/chat-move" && r.Method == http.MethodPost:
		s.moveRun(w, r, u)
	default:
		httpx.WriteError(w, r, httpx.ErrNotFound)
	}
}

type movedAt struct {
	ToMachine string `json:"toMachine"`
	ToName    string `json:"toName"`
	ToConv    string `json:"toConv"`
	At        *int64 `json:"at"`
}

// moveStatus says whether a chat can be moved from its machine and where it already went. The source runtime remembers
// that, so the hub keeps nothing: a machine that is away or old answers «off», the menu then offers nothing.
func (s *Service) moveStatus(w http.ResponseWriter, r *http.Request, u store.User) {
	q := r.URL.Query()
	src, fail := s.moveAdmit(r.Context(), u.ID, q.Get("machine"), failOff)
	if fail != nil {
		writeMoveFail(w, fail)
		return
	}
	conv := q.Get("conv")
	if !movePattern.MatchString(conv) {
		writeMoveFail(w, failBadReq)
		return
	}
	var st struct {
		Moved *struct {
			MachineID      string `json:"machineId"`
			Name           string `json:"name"`
			ConversationID string `json:"conversationId"`
		} `json:"moved"`
	}
	ctx, cancel := context.WithTimeout(r.Context(), moveProbeTime)
	defer cancel()
	if s.moveCall(ctx, u, src.m, "status", map[string]string{"conversationId": conv}, &st) != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false, "moved": nil})
		return
	}
	var moved *movedAt
	if st.Moved != nil {
		moved = &movedAt{ToMachine: st.Moved.MachineID, ToName: st.Moved.Name, ToConv: st.Moved.ConversationID}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "moved": moved})
}

type targetAgent struct {
	Provider string `json:"provider"`
	Profile  string `json:"profile"`
	Short    string `json:"short"`
	Models   any    `json:"models"`
}

type moveTarget struct {
	MachineID string        `json:"machineId"`
	Name      string        `json:"name"`
	OrgName   string        `json:"orgName"`
	Online    bool          `json:"online"`
	OK        bool          `json:"ok"`
	Code      string        `json:"code,omitempty"`
	Checks    any           `json:"checks"`
	FilesOK   bool          `json:"filesOk"`
	Agents    []targetAgent `json:"agents"`
}

// moveTargets is where a chat can go: every other machine of the person, with its organization, and what it can take.
func (s *Service) moveTargets(w http.ResponseWriter, r *http.Request, u store.User) {
	q := r.URL.Query()
	src, fail := s.moveAdmit(r.Context(), u.ID, q.Get("machine"), failOff)
	if fail != nil {
		writeMoveFail(w, fail)
		return
	}
	conv := q.Get("conv")
	if !movePattern.MatchString(conv) {
		writeMoveFail(w, failBadReq)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), moveProbeTime)
	defer cancel()
	var d struct {
		Project    *json.RawMessage `json:"project"`
		Clean      bool             `json:"clean"`
		Busy       bool             `json:"busy"`
		HasHistory bool             `json:"hasHistory"`
	}
	if fail := s.moveCall(ctx, u, src.m, "describe", map[string]string{"conversationId": conv}, &d); fail != nil {
		writeMoveFail(w, fail)
		return
	}
	source := map[string]any{"ok": true, "project": d.Project != nil, "dirty": !d.Clean, "busy": d.Busy}
	switch {
	case d.Busy:
		source["ok"], source["code"] = false, "agent_working"
	case !d.HasHistory:
		source["ok"], source["code"] = false, "no_history"
	}
	list, err := s.fleet(ctx, u.ID, false)
	if err != nil {
		if errors.Is(err, errFleetOff) {
			writeMoveFail(w, failOff)
			return
		}
		s.fail(w, r, err)
		return
	}
	var rest []FleetMachine
	for _, f := range list {
		if f.ID != src.m.ID {
			rest = append(rest, f)
		}
	}
	targets := make([]moveTarget, len(rest))
	var wg sync.WaitGroup
	for i, f := range rest {
		wg.Add(1)
		go func() {
			defer wg.Done()
			targets[i] = s.probeTarget(ctx, u, f, d.Project)
		}()
	}
	wg.Wait()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"source": source, "targets": targets})
}

// probeTarget asks a machine of the fleet what it would need to take the chat.
func (s *Service) probeTarget(ctx context.Context, u store.User, f FleetMachine, project *json.RawMessage) moveTarget {
	t := moveTarget{MachineID: f.ID, Name: f.Name, OrgName: f.OrgName, Online: f.Online, Checks: []any{}, Agents: []targetAgent{}}
	if !f.Online {
		t.Code = "machine_offline"
		return t
	}
	m, err := s.db.S().MachineByID(ctx, f.ID)
	if err != nil {
		t.Code = "machine_error"
		return t
	}
	var p struct {
		Checks  any           `json:"checks"`
		FilesOK bool          `json:"filesOk"`
		Agents  []targetAgent `json:"agents"`
	}
	if fail := s.moveCall(ctx, u, m, "preflight", map[string]any{"project": project}, &p); fail != nil {
		t.Online, t.Code = fail.Code != "machine_offline" && t.Online, fail.Code
		return t
	}
	if p.Checks != nil {
		t.Checks = p.Checks
	}
	t.FilesOK = p.FilesOK
	if p.Agents != nil {
		t.Agents = p.Agents
	}
	t.OK = len(t.Agents) > 0
	if !t.OK {
		t.Code = "agent_unavailable"
	}
	return t
}

type moveRequest struct {
	OpID            string `json:"opId"`
	MachineID       string `json:"machineId"`
	ConvID          string `json:"convId"`
	TargetMachineID string `json:"targetMachineId"`
	Provider        string `json:"provider"`
	Profile         string `json:"profile"`
	Model           struct {
		ID     string `json:"id"`
		Effort string `json:"effort"`
	} `json:"model"`
	WithoutFiles bool `json:"withoutFiles"`
}

// moveRun carries the chat. The source stays what it was until the target has the chat: whatever fails before the last
// step leaves the source active and the target clean.
func (s *Service) moveRun(w http.ResponseWriter, r *http.Request, u store.User) {
	var req moveRequest
	if httpx.Decode(r, &req) != nil || !movePattern.MatchString(req.OpID) || !movePattern.MatchString(req.ConvID) ||
		req.Provider == "" || req.Model.ID == "" {
		writeMoveFail(w, failBadReq)
		return
	}
	if req.MachineID == req.TargetMachineID {
		writeMoveFail(w, failSameVM)
		return
	}
	src, fail := s.moveAdmit(r.Context(), u.ID, req.MachineID, failNoSrc)
	if fail != nil {
		writeMoveFail(w, fail)
		return
	}
	dst, fail := s.moveAdmit(r.Context(), u.ID, req.TargetMachineID, failNoRight)
	if fail != nil {
		writeMoveFail(w, fail)
		return
	}
	ctx := r.Context()
	if s.reg.Get(dst.m.ID) == nil {
		writeMoveFail(w, offlineFail(dst.m))
		return
	}
	if s.reg.Get(src.m.ID) == nil {
		writeMoveFail(w, offlineFail(src.m))
		return
	}
	// The machines share no secret: the operation has a key of its own, made here and given to both ends.
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		writeMoveFail(w, &moveFail{Status: http.StatusInternalServerError, Code: "internal", Text: "Не удалось начать перенос"})
		return
	}
	key := base64.StdEncoding.EncodeToString(k)

	var exp struct {
		BundleEnc json.RawMessage `json:"bundleEnc"`
		Fence     string          `json:"fence"`
	}
	if fail := s.moveCall(ctx, u, src.m, "export", map[string]any{
		"opId": req.OpID, "conversationId": req.ConvID, "withoutFiles": req.WithoutFiles, "key": key,
	}, &exp); fail != nil {
		writeMoveFail(w, fail)
		return
	}
	var imp struct {
		ConversationID string `json:"conversationId"`
	}
	if fail := s.moveCall(ctx, u, dst.m, "import", map[string]any{
		"opId": req.OpID, "bundleEnc": exp.BundleEnc, "provider": req.Provider, "profile": req.Profile,
		"model": map[string]string{"id": req.Model.ID, "effort": req.Model.Effort}, "withoutFiles": req.WithoutFiles, "key": key,
	}, &imp); fail != nil {
		writeMoveFail(w, fail)
		return
	}
	// The last look at the source; a chat that moved on meanwhile stays where it is, and the target's copy goes.
	undo := func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), moveProbeTime)
		defer cancel()
		_ = s.moveCall(bg, u, src.m, "release", map[string]string{"opId": req.OpID, "conversationId": req.ConvID}, nil)
		_ = s.moveCall(bg, u, dst.m, "discard", map[string]string{"opId": req.OpID}, nil)
	}
	if fail := s.moveCall(ctx, u, src.m, "hold", map[string]string{
		"opId": req.OpID, "conversationId": req.ConvID, "fence": exp.Fence,
	}, nil); fail != nil {
		undo()
		writeMoveFail(w, fail)
		return
	}
	if fail := s.moveCall(ctx, u, src.m, "seal", map[string]any{
		"opId": req.OpID, "conversationId": req.ConvID,
		"to": map[string]string{"machineId": dst.m.ID, "name": dst.m.Name, "conversationId": imp.ConversationID},
	}, nil); fail != nil {
		undo()
		writeMoveFail(w, fail)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"targetMachineId": dst.m.ID, "targetName": dst.m.Name, "convId": imp.ConversationID,
	})
}
