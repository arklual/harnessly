package chatmove

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/handoff"
)

// Conversations is what a move needs of the conversation runtime.
type Conversations interface {
	Info(id string) (Info, bool)
	// Snapshot is the history as neutral JSONL and the fence it was read at; ErrNoHistory, or ErrTooBig past max.
	Snapshot(id string, max int64) ([]byte, string, error)
	Fence(id string) (string, bool)
	Open(in Open) (string, error)
	Delete(id string)
}

// Info is what a move reads of a conversation.
type Info struct {
	Title      string
	Cwd        string
	Busy       bool
	HasHistory bool
}

// Open is a conversation to start on the target.
type Open struct {
	Title    string
	Cwd      string
	History  []byte
	Provider string
	Profile  string
	Model    string
	Effort   string
}

// Errors of a Conversations.
var (
	ErrNoHistory = errors.New("chatmove: no history")
	ErrTooBig    = handoff.ErrArtifactTooBig
	// ErrAgent and ErrModel: the target's agent cannot start (not ready, not that profile) or does not know the model.
	ErrAgent = errors.New("chatmove: the agent is not available")
	ErrModel = errors.New("chatmove: the model is not available")
)

// Model is a model an agent offers.
type Model struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// Agent is a way to start a conversation on this machine: a CLI, or a profile through it.
type Agent struct {
	Provider string  `json:"provider"`
	Profile  string  `json:"profile"`
	Short    string  `json:"short"`
	Models   []Model `json:"models"`
}

// Service is the move of a runtime. Its HTTP face is Handler.
type Service struct {
	Conv   Conversations
	Agents func() []Agent
	Roots  []string
	Secret []byte
	Store  *Store
	// Home is where a move without files gets its folder.
	Home string

	locks sync.Map // opId -> *sync.Mutex
}

func (s *Service) lock(op string) func() {
	m, _ := s.locks.LoadOrStore(op, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Refusal is a move that did not happen, with the status and reason the cluster tells the person.
type Refusal struct {
	Status  int     `json:"-"`
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Checks  []Check `json:"checks,omitempty"`
}

func (r *Refusal) Error() string { return r.Code }

func refuse(status int, code, msg string, checks ...Check) *Refusal {
	return &Refusal{Status: status, Code: code, Message: msg, Checks: checks}
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// OpPattern is the shape of an operation id.
func validOp(id string) bool { return idPattern.MatchString(id) }

// DescribeResponse answers describe.
type DescribeResponse struct {
	Project    *Project `json:"project"`
	Clean      bool     `json:"clean"`
	Busy       bool     `json:"busy"`
	HasHistory bool     `json:"hasHistory"`
	Title      string   `json:"title"`
}

// Describe reads what a source has: where it worked, whether that is clean, whether it is at rest.
func (s *Service) Describe(ctx context.Context, conv string) (DescribeResponse, *Refusal) {
	info, ok := s.Conv.Info(conv)
	if !ok {
		return DescribeResponse{}, refuse(http.StatusNotFound, CodeNotFound, "Чат не найден")
	}
	out := DescribeResponse{Busy: info.Busy, HasHistory: info.HasHistory, Title: info.Title, Clean: true}
	if r := Inspect(ctx, info.Cwd); r != nil {
		p := r.Project
		out.Project, out.Clean = &p, r.Clean
	}
	return out, nil
}

// PreflightRequest asks a target whether it can take a conversation.
type PreflightRequest struct {
	Project  *Project `json:"project"`
	Provider string   `json:"provider"`
	Profile  string   `json:"profile"`
	Model    string   `json:"model"`
}

// PreflightResponse answers preflight: what was checked, whether the files can come, and the agents to continue with.
type PreflightResponse struct {
	Checks  []Check `json:"checks"`
	FilesOK bool    `json:"filesOk"`
	Agents  []Agent `json:"agents"`
}

func (s *Service) agents() []Agent {
	if s.Agents == nil {
		return []Agent{}
	}
	if a := s.Agents(); a != nil {
		return a
	}
	return []Agent{}
}

// agentChecks checks the agent a person picked against the ones the machine offers.
func agentChecks(offers []Agent, provider, profile, model string) []Check {
	var found *Agent
	for i := range offers {
		if offers[i].Provider == provider && offers[i].Profile == profile {
			found = &offers[i]
		}
	}
	if found == nil {
		return []Check{{ID: CheckAuth, Code: CodeAgent}}
	}
	checks := []Check{{ID: CheckAuth, OK: true}}
	for _, m := range found.Models {
		if m.ID == model {
			return append(checks, Check{ID: CheckModel, OK: true})
		}
	}
	return append(checks, Check{ID: CheckModel, Code: CodeModel})
}

// Preflight is the target's look at a move before anything leaves the source.
func (s *Service) Preflight(ctx context.Context, req PreflightRequest) PreflightResponse {
	out := PreflightResponse{Checks: []Check{}, FilesOK: true, Agents: s.agents()}
	if req.Project != nil {
		m := Locate(ctx, s.Roots, *req.Project)
		out.Checks, out.FilesOK = m.Checks, m.FilesOK()
	}
	if req.Provider != "" {
		out.Checks = append(out.Checks, agentChecks(out.Agents, req.Provider, req.Profile, req.Model)...)
	}
	return out
}

// ExportResponse answers export.
type ExportResponse struct {
	BundleEnc Envelope `json:"bundleEnc"`
	Fence     string   `json:"fence"`
}

// Export makes the bundle of a conversation at rest. Nothing changes on the source: it stays active until the cluster
// switches. A tree with uncommitted changes is refused (they are not carried), unless the move goes without files.
func (s *Service) Export(ctx context.Context, opID, conv string, withoutFiles bool) (ExportResponse, *Refusal) {
	if !validOp(opID) {
		return ExportResponse{}, refuse(http.StatusBadRequest, CodeBadBundle, "Неверная операция")
	}
	info, ok := s.Conv.Info(conv)
	if !ok {
		return ExportResponse{}, refuse(http.StatusNotFound, CodeNotFound, "Чат не найден")
	}
	if s.Store.Blocked(conv) {
		return ExportResponse{}, refuse(http.StatusConflict, CodeAlreadyMoved, "Чат уже продолжен на другой ВМ")
	}
	if info.Busy {
		return ExportResponse{}, refuse(http.StatusConflict, CodeBusy, "Агент ещё работает: дождитесь ответа или остановите его")
	}
	if !info.HasHistory {
		return ExportResponse{}, refuse(http.StatusUnprocessableEntity, CodeNoHistory, "В чате пока нет истории")
	}
	var project *Project
	if !withoutFiles {
		if r := Inspect(ctx, info.Cwd); r != nil {
			if !r.Clean {
				return ExportResponse{}, refuse(http.StatusConflict, CodeDirtySource,
					"В папке чата есть незакоммиченные изменения: перенос их пока не умеет. Закоммитьте их или продолжите без локальных файлов")
			}
			p := r.Project
			project = &p
		}
	}
	history, fence, err := s.Conv.Snapshot(conv, MaxHistory)
	switch {
	case errors.Is(err, ErrNoHistory):
		return ExportResponse{}, refuse(http.StatusUnprocessableEntity, CodeNoHistory, "В чате пока нет истории")
	case errors.Is(err, ErrTooBig):
		return ExportResponse{}, refuse(http.StatusRequestEntityTooLarge, CodeTooBig, "История слишком большая для переноса")
	case err != nil:
		return ExportResponse{}, refuse(http.StatusInternalServerError, CodeInternal, "Не удалось прочитать историю")
	}
	raw, err := json.Marshal(Bundle{V: BundleVersion, Title: info.Title, History: string(history), Project: project})
	if err != nil {
		return ExportResponse{}, refuse(http.StatusInternalServerError, CodeInternal, "Не удалось собрать перенос")
	}
	env, err := Encrypt(s.Secret, opID, Export, raw)
	if err != nil {
		return ExportResponse{}, refuse(http.StatusInternalServerError, CodeInternal, "Не удалось зашифровать перенос")
	}
	return ExportResponse{BundleEnc: env, Fence: fence}, nil
}

// ImportRequest is a bundle for a target to continue on.
type ImportRequest struct {
	OpID         string   `json:"opId"`
	BundleEnc    Envelope `json:"bundleEnc"`
	Provider     string   `json:"provider"`
	Profile      string   `json:"profile"`
	Model        ModelRef `json:"model"`
	WithoutFiles bool     `json:"withoutFiles"`
}

// ModelRef is the model a move continues on.
type ModelRef struct {
	ID     string `json:"id"`
	Effort string `json:"effort"`
}

// ImportResponse answers import.
type ImportResponse struct {
	ConversationID string `json:"conversationId"`
	Existing       bool   `json:"existing"`
}

func firstFailed(checks []Check) *Check {
	for i := range checks {
		if !checks[i].OK {
			return &checks[i]
		}
	}
	return nil
}

var refusalText = map[string]string{
	CodeNoProject: "На этой ВМ нет этого репозитория. Можно продолжить без локальных файлов",
	CodeCommit:    "На этой ВМ нет нужного коммита. Можно продолжить без локальных файлов",
	CodeDirty:     "Папка репозитория на этой ВМ с изменениями. Можно продолжить без локальных файлов",
	CodeAgent:     "Агент на этой ВМ недоступен",
	CodeModel:     "Этой модели нет на этой ВМ",
}

// Import starts the conversation of a move on this machine, once per operation: a repeated import answers the same
// conversation. Whatever fails leaves nothing behind.
func (s *Service) Import(ctx context.Context, req ImportRequest) (ImportResponse, *Refusal) {
	if !validOp(req.OpID) {
		return ImportResponse{}, refuse(http.StatusBadRequest, CodeBadBundle, "Неверная операция")
	}
	defer s.lock(req.OpID)()
	if rec, ok := s.Store.op(req.OpID); ok {
		if _, alive := s.Conv.Info(rec.TargetID); alive {
			return ImportResponse{ConversationID: rec.TargetID, Existing: true}, nil
		}
		s.Store.dropOp(req.OpID)
	}
	plain, err := Decrypt(s.Secret, req.OpID, Import, req.BundleEnc)
	if err != nil {
		return ImportResponse{}, refuse(http.StatusBadRequest, CodeBadBundle, "Перенос не прошёл проверку")
	}
	bundle, err := DecodeBundle(plain)
	if err != nil {
		return ImportResponse{}, refuse(http.StatusBadRequest, CodeBadBundle, "Перенос не прошёл проверку")
	}
	if bad := firstFailed(agentChecks(s.agents(), req.Provider, req.Profile, req.Model.ID)); bad != nil {
		return ImportResponse{}, refuse(http.StatusConflict, bad.Code, refusalText[bad.Code], *bad)
	}
	var rec opRecord
	cwd := ""
	short := req.OpID
	if len(short) > 8 {
		short = short[:8]
	}
	name := "move-" + strings.ToLower(short)
	if bundle.Project != nil && !req.WithoutFiles {
		m := Locate(ctx, s.Roots, *bundle.Project)
		if bad := firstFailed(m.Checks); bad != nil || m.Repo == nil {
			c := Check{ID: CheckProject, Code: CodeNoProject}
			if bad != nil {
				c = *bad
			}
			return ImportResponse{}, refuse(http.StatusConflict, c.Code, refusalText[c.Code], m.Checks...)
		}
		dir, err := AddWorktree(ctx, m.Repo, name, bundle.Project.Commit, bundle.Project.RelDir)
		if err != nil {
			return ImportResponse{}, refuse(http.StatusInternalServerError, CodeInternal, "Не удалось создать рабочую папку")
		}
		cwd = dir
		rec = opRecord{Repo: m.Repo.Root, Worktree: filepath.Join(m.Repo.Root, ".worktrees", name), Branch: "agent/" + name}
	} else {
		dir := filepath.Join(s.Home, "moved-"+strings.ToLower(short))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return ImportResponse{}, refuse(http.StatusInternalServerError, CodeInternal, "Не удалось создать рабочую папку")
		}
		cwd, rec = dir, opRecord{Dir: dir}
	}
	id, err := s.Conv.Open(Open{Title: bundle.Title, Cwd: cwd, History: []byte(bundle.History), Provider: req.Provider,
		Profile: req.Profile, Model: req.Model.ID, Effort: req.Model.Effort})
	if err != nil {
		s.cleanup(ctx, rec)
		code, status := CodeInternal, http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrAgent):
			code, status = CodeAgent, http.StatusConflict
		case errors.Is(err, ErrModel):
			code, status = CodeModel, http.StatusConflict
		}
		msg := refusalText[code]
		if msg == "" {
			msg = "Не удалось запустить чат на этой ВМ"
		}
		return ImportResponse{}, refuse(status, code, msg)
	}
	rec.TargetID = id
	if err := s.Store.putOp(req.OpID, rec); err != nil {
		s.Conv.Delete(id)
		s.cleanup(ctx, rec)
		return ImportResponse{}, refuse(http.StatusInternalServerError, CodeInternal, "Не удалось запомнить перенос")
	}
	return ImportResponse{ConversationID: id}, nil
}

// cleanup removes what an import made around a conversation that is not to be: its worktree and branch, or its folder
// when it is still empty.
func (s *Service) cleanup(ctx context.Context, rec opRecord) {
	switch {
	case rec.Worktree != "" && rec.Repo != "":
		_, _ = git(ctx, rec.Repo, "worktree", "remove", "--force", rec.Worktree)
		if rec.Branch != "" {
			_, _ = git(ctx, rec.Repo, "branch", "-D", rec.Branch)
		}
	case rec.Dir != "":
		_ = os.Remove(rec.Dir) // only an empty folder goes
	}
}

// Discard undoes an import whose move did not happen. Idempotent.
func (s *Service) Discard(ctx context.Context, opID string) *Refusal {
	if !validOp(opID) {
		return refuse(http.StatusBadRequest, CodeBadBundle, "Неверная операция")
	}
	defer s.lock(opID)()
	rec, ok := s.Store.op(opID)
	if !ok {
		return nil
	}
	if rec.TargetID != "" {
		s.Conv.Delete(rec.TargetID)
	}
	s.cleanup(ctx, rec)
	s.Store.dropOp(opID)
	return nil
}

// Hold stops the source taking messages for a moment, if it is still what was exported: at rest and its transcript
// unchanged. It is the last look before the cluster switches.
func (s *Service) Hold(opID, conv, fence string) *Refusal {
	if !validOp(opID) || fence == "" {
		return refuse(http.StatusBadRequest, CodeBadBundle, "Неверная операция")
	}
	info, ok := s.Conv.Info(conv)
	if !ok {
		return refuse(http.StatusNotFound, CodeNotFound, "Чат не найден")
	}
	if info.Busy {
		return refuse(http.StatusConflict, CodeBusy, "Агент начал работать: перенос отменён, чат остался здесь")
	}
	if now, ok := s.Conv.Fence(conv); !ok || now != fence {
		return refuse(http.StatusConflict, CodeChanged, "В чате появились новые сообщения: перенос отменён, чат остался здесь")
	}
	if err := s.Store.hold(conv, opID); err != nil {
		return refuse(http.StatusConflict, CodeAlreadyMoved, "Чат уже продолжен на другой ВМ")
	}
	// A message that slipped in between the look and the hold shows in the fence again.
	if now, ok := s.Conv.Fence(conv); !ok || now != fence {
		_ = s.Store.release(conv, opID)
		return refuse(http.StatusConflict, CodeChanged, "В чате появились новые сообщения: перенос отменён, чат остался здесь")
	}
	return nil
}

// Seal makes the source read-only and points it at where the work went on. Idempotent for the same operation.
func (s *Service) Seal(opID, conv string, to Pointer) *Refusal {
	if !validOp(opID) || !validOp(to.ConversationID) || to.MachineID == "" {
		return refuse(http.StatusBadRequest, CodeBadBundle, "Неверная операция")
	}
	if _, ok := s.Conv.Info(conv); !ok {
		return refuse(http.StatusNotFound, CodeNotFound, "Чат не найден")
	}
	to.Name = strings.TrimSpace(to.Name)
	if err := s.Store.seal(conv, opID, to); err != nil {
		return refuse(http.StatusConflict, CodeAlreadyMoved, "Чат уже продолжен на другой ВМ")
	}
	return nil
}

// Release lifts the hold of an operation that did not switch.
func (s *Service) Release(opID, conv string) *Refusal {
	if !validOp(opID) {
		return refuse(http.StatusBadRequest, CodeBadBundle, "Неверная операция")
	}
	_ = s.Store.release(conv, opID)
	return nil
}

// Handler serves POST .../move/<step>: JSON in, JSON out, a refusal as {code, message, checks} with its status.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		step := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		limit := int64(1 << 16)
		if step == "import" {
			limit = MaxBody
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		if err != nil || int64(len(body)) > limit {
			write(w, refuse(http.StatusRequestEntityTooLarge, CodeTooBig, "Запрос слишком большой"))
			return
		}
		write(w, s.dispatch(ctx, step, body))
	})
}

func write(w http.ResponseWriter, v any) {
	status := http.StatusOK
	if r, ok := v.(*Refusal); ok {
		status = r.Status
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(body []byte, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	return dec.Decode(v) == nil
}

func (s *Service) dispatch(ctx context.Context, step string, body []byte) any {
	bad := refuse(http.StatusBadRequest, CodeBadBundle, "Неверный запрос")
	switch step {
	case "describe":
		var q struct {
			ConversationID string `json:"conversationId"`
		}
		if !decode(body, &q) {
			return bad
		}
		out, ref := s.Describe(ctx, q.ConversationID)
		return orRefusal(out, ref)
	case "preflight":
		var q PreflightRequest
		if !decode(body, &q) {
			return bad
		}
		return s.Preflight(ctx, q)
	case "export":
		var q struct {
			OpID           string `json:"opId"`
			ConversationID string `json:"conversationId"`
			WithoutFiles   bool   `json:"withoutFiles"`
		}
		if !decode(body, &q) {
			return bad
		}
		out, ref := s.Export(ctx, q.OpID, q.ConversationID, q.WithoutFiles)
		return orRefusal(out, ref)
	case "import":
		var q ImportRequest
		if !decode(body, &q) {
			return bad
		}
		out, ref := s.Import(ctx, q)
		return orRefusal(out, ref)
	case "hold":
		var q struct {
			OpID           string `json:"opId"`
			ConversationID string `json:"conversationId"`
			Fence          string `json:"fence"`
		}
		if !decode(body, &q) {
			return bad
		}
		return orRefusal(struct{}{}, s.Hold(q.OpID, q.ConversationID, q.Fence))
	case "seal":
		var q struct {
			OpID           string  `json:"opId"`
			ConversationID string  `json:"conversationId"`
			To             Pointer `json:"to"`
		}
		if !decode(body, &q) {
			return bad
		}
		return orRefusal(struct{}{}, s.Seal(q.OpID, q.ConversationID, q.To))
	case "release":
		var q struct {
			OpID           string `json:"opId"`
			ConversationID string `json:"conversationId"`
		}
		if !decode(body, &q) {
			return bad
		}
		return orRefusal(struct{}{}, s.Release(q.OpID, q.ConversationID))
	case "discard":
		var q struct {
			OpID string `json:"opId"`
		}
		if !decode(body, &q) {
			return bad
		}
		return orRefusal(struct{}{}, s.Discard(ctx, q.OpID))
	}
	return refuse(http.StatusNotFound, CodeNotFound, "Нет такого шага")
}

func orRefusal(v any, ref *Refusal) any {
	if ref != nil {
		return ref
	}
	return v
}
