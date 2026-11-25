package designaccess

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
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
	if m.SpaceID == "" {
		return fail("store_off", "Хранилище дизайнов не подключено")
	}
	project, sub, _ := strings.Cut(strings.Trim(rest, "/"), "/")
	if !projectIDRe.MatchString(project) {
		return fail("not_found", "Проект не найден")
	}
	section, file, _ := strings.Cut(sub, "/")
	// A board's address with its key (filekey.go): the files under it are read as those of f/.
	keyed := false
	if section == "k" {
		key, after, _ := strings.Cut(file, "/")
		inner, rest, _ := strings.Cut(after, "/")
		if inner != "f" || !s.files.check(key, u.ID, project, s.now()) {
			return fail("not_found", "Файл не найден")
		}
		section, file, keyed = "f", rest, true
	}
	ctx := r.Context()
	if s.view == nil {
		if section != "f" && section != "index.json" {
			return fail("store_off", "Хранилище дизайнов не подключено")
		}
		var err error
		m, err = s.scopedHolder(ctx, m, u, project, "")
		if err != nil {
			return err
		}
	}
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
		if s.view == nil {
			var segs []string
			if section == "f" {
				var ok bool
				segs, ok = segments(file)
				if !ok || !servedSegment(segs[0]) {
					return fail("not_found", "Файл не найден")
				}
				for _, seg := range segs {
					if strings.Contains(seg, "/") {
						return fail("not_found", "Файл не найден")
					}
				}
				if !keyed && s.files != nil && storeNavigation(r) {
					w.Header().Set("Cache-Control", "no-store")
					w.Header().Set("Location", keyedLocation(segs, s.files.mint(u.ID, project, s.now()), r.URL.RawQuery))
					w.WriteHeader(http.StatusFound)
					return nil
				}
			} else if file != "" {
				return fail("not_found", "Маршрут не найден")
			}
			if s.reg == nil {
				return fail("unavailable", "Машина проекта недоступна")
			}
			// The record holder attested this project. Never select a runtime supplied by the caller.
			res, err := s.machineFetch(ctx, m.ID, hubproto.DesignPublicTokenRequest{Project: project}, segs, r)
			if err != nil {
				return fail("unavailable", "Машина проекта недоступна")
			}
			if res.StatusCode != http.StatusOK {
				res.Body.Close()
				return fail("not_found", "Файл не найден")
			}
			h := w.Header()
			h.Set("Cache-Control", "private, no-store")
			h.Set("Content-Type", safeType(res.Header.Get("Content-Type")))
			passEncoding(h, res)
			return copyBody(isolated(w, r), res, maxFile, r.Method == http.MethodHead)
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
			out := map[string]any{"version": head, "rev": storerev.ID(head)}
			// The key of the board addresses, for a page that builds them itself (filekey.go).
			if key := s.files.mint(u.ID, project, s.now()); key != "" {
				out["key"] = key
			}
			writeJSON(w, http.StatusOK, out)
			return nil
		case "events":
			s.streamRevs(w, r, m.SpaceID, project, head)
			return nil
		case "index.json":
			if file != "" {
				return fail("not_found", "Маршрут не найден")
			}
			return s.view.Serve(isolated(w, r), r, m.SpaceID, project, head, nil, nil, false)
		case "f":
			segs, ok := segments(file)
			if !ok || !servedSegment(segs[0]) {
				return fail("not_found", "Файл не найден")
			}
			// A board opened by its plain address goes to its keyed one, so its own files reach the hub.
			if !keyed && s.files != nil && storeNavigation(r) {
				h := w.Header()
				h.Set("Location", keyedLocation(segs, s.files.mint(u.ID, project, s.now()), r.URL.RawQuery))
				h.Set("Cache-Control", "no-store")
				h.Set("Referrer-Policy", "strict-origin")
				w.WriteHeader(http.StatusFound)
				return nil
			}
			sourced := false
			read := func(w http.ResponseWriter, r *http.Request, segs []string) error {
				fell, err := s.serveOrSource(w, r, t, u, m.SpaceID, project, head, segs)
				sourced = sourced || fell
				return err
			}
			if isStylesheet(segs) && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
				// A revision decides its sheet: worked out once per replica, not on every open of every board.
				key := sheetKey(m.SpaceID, project, head, segs)
				if out := s.sheets.get(key); out != nil {
					out.write(isolated(w, r), r)
					return nil
				}
				out, pass, err := buildSheet(r, segs, read)
				if err != nil {
					return err
				}
				if pass != nil {
					copyAnswer(isolated(w, r), r, pass)
					return nil
				}
				if !sourced {
					s.sheets.put(key, out)
				}
				out.write(isolated(w, r), r)
				return nil
			}
			return read(isolated(w, r), r, segs)
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
// sandboxCORP lets a sandboxed document load its own files. Its origin is opaque (null), so the browser asks for its
// support.js and styles as «cross-site», and «same-origin» would block them. Only such a request gets «cross-origin»:
// a page of another site gets no session cookie (it is SameSite=Lax) and is refused before this, while a page of a
// sibling host of the same site, which does carry the cookie, asks as «same-site» and keeps «same-origin».
func sandboxCORP(r *http.Request) string {
	if r != nil && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return "cross-origin"
	}
	return "same-origin"
}

func isolated(w http.ResponseWriter, r *http.Request) http.ResponseWriter {
	return &isolatedWriter{ResponseWriter: w, corp: sandboxCORP(r)}
}

type isolatedWriter struct {
	http.ResponseWriter
	done bool
	corp string
}

func (i *isolatedWriter) WriteHeader(code int) {
	if !i.done {
		i.done = true
		h := i.Header()
		h.Set("Content-Type", safeType(h.Get("Content-Type")))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "sandbox allow-scripts allow-popups allow-forms allow-modals; frame-ancestors 'self'")
		h.Set("Cross-Origin-Resource-Policy", i.corp)
		// The board's own requests leave its opaque origin as cross-site ones: the origin in their Referer is how the hub
		// tells them from another site's (proxy sandboxStoreFile).
		h.Set("Referrer-Policy", "strict-origin")
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
