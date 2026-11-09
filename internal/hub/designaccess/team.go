package designaccess

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/storerev"
)

// The team's own read and write of a project in the store, through the hub: the person's role is the hub's
// (team, membership, the owner), the files are the store's, and no machine takes part — the author's may be off, and
// so may the reader's. Under a machine the person may open (it names the space), <prefix>/design/store/<project>/:
//
//	GET  head           {version, rev}: the project's newest revision
//	GET  events         the stream «the project is at revision N», as a live guest link has it
//	GET  index.json     the project's boards at its newest revision
//	GET  f/<path>       a file of the boards' folder at the newest revision, as a guest link reads it
//	PUT  file/<path>    commit the body as the next revision of a file of the project's folder (edit; the owner file
//	                    only by the owner); X-Store-Base: the file's revision the edit was made on, 0 for a new file
//	DELETE file/<path>  commit the file's deletion (edit), with X-Store-Base too
//
// A write on a stale base is 409 with the file's revision now; the editor reads it again and decides.

// StoreBaseHeader carries the revision of the file a write was made on (0 for a new file).
const StoreBaseHeader = "X-Store-Base"

func (s *Service) serveStore(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, rest string) {
	if err := s.storeRoute(w, r, u, m, rest); err != nil {
		s.writeError(w, err)
	}
}

func (s *Service) storeRoute(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, rest string) error {
	if s.view == nil || m.SpaceID == "" {
		return fail("not_found", "Хранилище дизайнов не подключено")
	}
	project, sub, _ := strings.Cut(strings.Trim(rest, "/"), "/")
	if !projectIDRe.MatchString(project) {
		return fail("not_found", "Проект не найден")
	}
	section, file, _ := strings.Cut(sub, "/")
	ctx := r.Context()
	t, err := s.read(ctx, m)
	if err != nil {
		return err
	}
	p, stored, err := t.effective(project)
	if err != nil {
		return err
	}
	// Only a project the hub has a row for: an unrecorded one is still the machines' (its owner file has not reached
	// the store), and a write through the hub would fill its folder before the owner file can record it.
	if !stored {
		return fail("not_found", "Проект не найден")
	}
	f, err := t.fold(p, u.Email)
	if err != nil {
		return err
	}
	// A project closed to the person is not there for them.
	if f.Role == "" {
		return fail("not_found", "Проект не найден")
	}
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	if section != "file" {
		if !read {
			return fail("bad_request", "Метод не поддерживается")
		}
		head, err := s.view.Head(ctx, m.SpaceID, project)
		if err != nil {
			return err
		}
		if head == 0 {
			return fail("not_found", "Проекта ещё нет в хранилище")
		}
		switch section {
		case "head":
			writeJSON(w, http.StatusOK, map[string]any{"version": head, "rev": storerev.ID(head)})
			return nil
		case "events":
			s.streamRevs(w, r, m.SpaceID, project, head)
			return nil
		case "index.json":
			if file != "" {
				return fail("not_found", "Маршрут не найден")
			}
			return s.view.Serve(isolated(w), r, m.SpaceID, project, head, nil, nil, false)
		case "f":
			segs, ok := segments(file)
			if !ok || strings.HasPrefix(segs[0], "__") && segs[0] != "__guest" && segs[0] != "__vendor" {
				return fail("not_found", "Файл не найден")
			}
			return s.view.Serve(isolated(w), r, m.SpaceID, project, head, segs, nil, false)
		}
		return fail("not_found", "Маршрут не найден")
	}
	return s.storeWrite(w, r, u, m, project, file, f)
}

func (s *Service) storeWrite(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, project, file string, f folded) error {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		return fail("bad_request", "Метод не поддерживается")
	}
	if rank(f.Role) < rank(roleEdit) {
		return fail("forbidden", "Править проект может только редактор")
	}
	segs, ok := segments(file)
	if !ok {
		return fail("bad_request", "Некорректный путь файла")
	}
	rel := strings.Join(segs, "/")
	// Who owns a recorded project is the hub's word: its owner file changes only with the owner, as the sync has it.
	if strings.EqualFold(rel, filestore.OwnerFile) && f.Role != roleOwner {
		return fail("forbidden", "Файл владельца меняет только владелец")
	}
	base, err := strconv.ParseInt(r.Header.Get(StoreBaseHeader), 10, 64)
	if err != nil || base < 0 {
		return fail("bad_request", "Нужен заголовок "+StoreBaseHeader+": ревизия, на которой сделана правка (0 — новый файл)")
	}
	var data []byte
	if r.Method == http.MethodPut {
		data, err = io.ReadAll(io.LimitReader(r.Body, filestore.MaxFileSize+1))
		if err != nil {
			return fail("bad_request", "Тело не прочитано")
		}
		if int64(len(data)) > filestore.MaxFileSize {
			return fail("bad_request", "Файл больше 50 МБ")
		}
		if data == nil {
			data = []byte{}
		}
	}
	out, err := s.view.Write(r.Context(), m.SpaceID, project, rel, base, data, normEmail(u.Email))
	switch {
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Файл уже изменили: прочитайте его заново", "code": "conflict", "rev": out.Rev})
		return nil
	case errors.Is(err, ErrBadPath):
		return fail("bad_request", "Этот файл в хранилище не хранится")
	case errors.Is(err, ErrReadOnly):
		return fail("unavailable", "Хранилище сейчас только для чтения")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"rev": out.Rev, "version": out.Head})
	return nil
}

// isolated answers a file of the store on the hub's own origin as an inert one: a board's scripts run, if at all, in
// a sandbox of an opaque origin (no cookies, no storage, no calls with the person's session), and nothing is sniffed
// into another type.
func isolated(w http.ResponseWriter) http.ResponseWriter { return &isolatedWriter{ResponseWriter: w} }

type isolatedWriter struct {
	http.ResponseWriter
	done bool
}

func (i *isolatedWriter) WriteHeader(code int) {
	if !i.done {
		i.done = true
		h := i.Header()
		h.Set("Content-Type", safeType(h.Get("Content-Type")))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "sandbox allow-scripts allow-popups allow-forms allow-modals; frame-ancestors 'self'")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if h.Get("Cache-Control") == "" {
			h.Set("Cache-Control", "private, no-cache")
		}
	}
	i.ResponseWriter.WriteHeader(code)
}

func (i *isolatedWriter) Write(b []byte) (int, error) {
	if !i.done {
		i.WriteHeader(http.StatusOK)
	}
	return i.ResponseWriter.Write(b)
}

func (i *isolatedWriter) Flush() {
	if f, ok := i.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
