package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/features"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

var errSafeOff = httpx.New(http.StatusNotFound, "not_enabled", "Сейф хаба не включён")

// safeAudit is what the journal says of a request: what was done to which secret by whose machine, never a value.
var safeAudit = map[string]string{
	hubproto.SecretsRead:   "secret.read",
	hubproto.SecretsWrite:  "secret.write",
	hubproto.SecretsDelete: "secret.delete",
	hubproto.SecretsRef:    "secret.ref",
}

// agentSecrets is POST /agent/secrets: a machine, proving itself with its own key over the body, reads or changes the
// secrets of its owner in the hub's safe (docs/safe.md, "The hub's safe"). The folder is the hub's choice, from the
// machine it verified; a value read comes back sealed to the machine's key.
func (h *Hub) agentSecrets(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, hubproto.MaxSecretsBody+1))
	if err != nil {
		return httpx.Invalid("body", "Не удалось прочитать запрос")
	}
	if len(body) > hubproto.MaxSecretsBody {
		return httpx.New(http.StatusRequestEntityTooLarge, "too_large", "Слишком большой запрос")
	}
	m, err := h.Dialer.AdmitSecrets(r, body)
	if err != nil {
		return err
	}
	ctx := r.Context()
	sp, err := h.db.S().SpaceByID(ctx, m.SpaceID)
	if err != nil {
		return err
	}
	org := sp.OrgID
	if org == "" {
		org = "space:" + sp.ID
	}
	if h.Safe == nil || !h.Features.On(ctx, org, features.SecretsVault) {
		return errSafeOff
	}
	var req hubproto.SecretsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return httpx.Invalid("body", "Некорректный запрос")
	}
	names, data, err := h.Safe.Do(ctx, m.CreatedBy, req)
	if action, ok := safeAudit[req.Op]; ok {
		target := req.Name
		if req.Op == hubproto.SecretsRef {
			target = req.Path
		}
		outcome := "ok"
		if err != nil {
			outcome = secrets.ReasonOf(err)
		}
		// The journal is the hub's record of who got what; a failure to write it is logged, not shown to the machine.
		if aerr := h.db.S().AddAudit(ctx, &store.AuditEvent{
			OrgID: sp.OrgID, SpaceID: sp.ID, ActorID: m.CreatedBy, ScopeKind: store.ScopeTenant, ScopeID: sp.ID,
			Action: action, TargetKind: "secret", TargetID: target,
			After: mustJSON(map[string]string{"machine": m.ID, "outcome": outcome}),
		}); aerr != nil {
			h.log.Warn("safe: journal not written", "machine", m.ID, "action", action, "err", aerr)
		}
	}
	if err != nil {
		h.log.Info("safe: refused", "machine", m.ID, "op", req.Op, "reason", secrets.ReasonOf(err), "err", err)
		return safeError(err)
	}
	w.Header().Set("Cache-Control", "no-store")
	if data == nil {
		httpx.WriteJSON(w, http.StatusOK, hubproto.SecretsAnswer{Names: names})
		return nil
	}
	encPub, err := base64.StdEncoding.DecodeString(m.EncPub)
	if err != nil {
		return httpx.New(http.StatusConflict, "no_key", "У машины нет ключа для сейфа")
	}
	auth, _ := hubproto.ParseDialHeader(r.Header.Get("Authorization"))
	plain, _ := json.Marshal(data)
	sealed, err := hubproto.SealSecret(encPub, m.ID, auth.Nonce, plain)
	if err != nil {
		return httpx.New(http.StatusRequestEntityTooLarge, "too_large", "Значение не помещается")
	}
	httpx.WriteJSON(w, http.StatusOK, hubproto.SecretsAnswer{Sealed: sealed})
	return nil
}

// safeError is the answer to a refusal, its reason as the code (the agent turns it back into the same refusal).
func safeError(err error) error {
	reason := secrets.ReasonOf(err)
	status := map[string]int{
		secrets.ReasonNotFound:    http.StatusNotFound,
		secrets.ReasonDenied:      http.StatusForbidden,
		secrets.ReasonBadName:     http.StatusBadRequest,
		secrets.ReasonEmpty:       http.StatusBadRequest,
		secrets.ReasonTooLarge:    http.StatusRequestEntityTooLarge,
		secrets.ReasonUnavailable: http.StatusServiceUnavailable,
	}[reason]
	if status == 0 {
		status, reason = http.StatusBadGateway, secrets.ReasonFailed
	}
	return httpx.New(status, "safe_"+reason, "Сейф отказал: "+reason)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
