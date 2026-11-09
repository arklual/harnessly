package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A person's agent memory for all of the person's machines (docs/hub/memory-sync.md). The machine proves itself with
// its key; whose memory it is the hub decides alone: the person who registered the machine, while they are still a
// member of its space. Nothing in the request can name another person.

const (
	memoryPerMinute = 240
	memoryMaxBody   = hubproto.MemoryMaxNote*2 + 64<<10
)

var errMemoryNoOwner = httpx.New(http.StatusForbidden, "memory_no_owner", "У машины нет владельца, чью память можно синхронизировать")

// memoryOwner is the machine and the person whose memory it reads and writes.
func (h *Hub) memoryOwner(r *http.Request) (store.Machine, string, error) {
	m, err := h.Dialer.AdmitMemory(r)
	if err != nil {
		return store.Machine{}, "", err
	}
	if ok, _ := h.limiter.Allow("memory:"+m.ID, memoryPerMinute, time.Minute); !ok {
		return store.Machine{}, "", httpx.New(http.StatusTooManyRequests, "rate_limited", "Слишком много запросов")
	}
	if m.CreatedBy == "" || m.SpaceID == "" {
		return store.Machine{}, "", errMemoryNoOwner
	}
	if role, err := h.db.S().Role(r.Context(), m.SpaceID, m.CreatedBy); err != nil || role == "" {
		return store.Machine{}, "", errMemoryNoOwner
	}
	return m, m.CreatedBy, nil
}

func (h *Hub) memoryNoteOut(ctx context.Context, n store.MemoryNote) hubproto.MemoryNote {
	out := hubproto.MemoryNote{Project: n.Project, Name: n.Name, Body: n.Body, Version: n.Version, Deleted: n.Deleted,
		UpdatedAt: store.Ms(n.UpdatedAt)}
	if m, err := h.db.S().MachineByID(ctx, n.MachineID); err == nil {
		out.Machine = m.Name
	}
	return out
}

// memoryPull is POST /agent/memory/pull: every note of the asked projects of the machine's person.
func (h *Hub) memoryPull(w http.ResponseWriter, r *http.Request) error {
	_, person, err := h.memoryOwner(r)
	if err != nil {
		return err
	}
	var body hubproto.MemoryPull
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || len(body.Projects) > hubproto.MemoryMaxProjects {
		return httpx.Invalid("projects", "Не список проектов")
	}
	for _, p := range body.Projects {
		if !hubproto.MemoryProjectOK(p) {
			return httpx.Invalid("projects", "Не проект")
		}
	}
	notes, err := h.db.S().MemoryNotes(r.Context(), person, body.Projects)
	if err != nil {
		return err
	}
	names := map[string]string{}
	out := hubproto.MemoryNotes{Notes: make([]hubproto.MemoryNote, 0, len(notes))}
	for _, n := range notes {
		name, ok := names[n.MachineID]
		mid := n.MachineID
		if ok {
			n.MachineID = ""
		}
		o := h.memoryNoteOut(r.Context(), n)
		if ok {
			o.Machine = name
		}
		names[mid] = o.Machine
		out.Notes = append(out.Notes, o)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// memoryPut is POST /agent/memory/put: write or delete one note of the machine's person, if it did not change since the
// version the machine saw; otherwise the answer is the newer note and nothing is written.
func (h *Hub) memoryPut(w http.ResponseWriter, r *http.Request) error {
	m, person, err := h.memoryOwner(r)
	if err != nil {
		return err
	}
	var body hubproto.MemoryPut
	if err := json.NewDecoder(io.LimitReader(r.Body, memoryMaxBody)).Decode(&body); err != nil {
		return httpx.Invalid("note", "Не заметка")
	}
	switch {
	case !hubproto.MemoryProjectOK(body.Project):
		return httpx.Invalid("project", "Не проект")
	case !hubproto.MemoryNameOK(body.Name):
		return httpx.Invalid("name", "Имя заметки — буквы, цифры, точка, дефис, подчёркивание и .md")
	case len(body.Body) > hubproto.MemoryMaxNote:
		return httpx.Invalid("body", "Заметка больше 256 КБ")
	}
	n := store.MemoryNote{UserID: person, Project: body.Project, Name: body.Name, Body: body.Body,
		Version: hubproto.MemoryVersion(body.Body), MachineID: m.ID, UpdatedAt: time.Now()}
	if body.Delete {
		n.Body, n.Version, n.Deleted = "", hubproto.MemoryTombstone, true
	}
	kept, err := h.db.S().PutMemoryNote(r.Context(), n, body.Base, hubproto.MemoryMaxNotes)
	var res hubproto.MemoryPutResult
	switch {
	case errors.Is(err, store.ErrMemoryChanged):
		c := h.memoryNoteOut(r.Context(), kept)
		res.Conflict = &c
	case errors.Is(err, store.ErrMemoryFull):
		return httpx.New(http.StatusUnprocessableEntity, "memory_full", "В проекте слишком много заметок")
	case err != nil:
		return err
	default:
		k := h.memoryNoteOut(r.Context(), kept)
		res.Note = &k
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}
