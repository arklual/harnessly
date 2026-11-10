package proxy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The person's dictation recordings («Записи» of the composer, docs/hub/voice-takes.md, HRN-989): what the browser keeps
// goes here once a take is over, so it outlives the browser and the machine. The audio is kept voiceAudioKept, the text
// for good. The API is the cluster gateway's (src/cluster/voice-takes.ts), under the workspace's platform API, and only
// ever of the signed-in person's own takes (HUB_VOICE_TAKES; off: 404, the page reads it as «not here»):
//
//	GET    <platform api>/voice/takes                                       → {takes}, newest first
//	PUT    <platform api>/voice/takes/<id>?machine=&at=&ms=&state=  (body: the audio) → {take}
//	PATCH  <platform api>/voice/takes/<id>                {text?, state?}   → {take}
//	DELETE <platform api>/voice/takes/<id>                                   → {ok}
//	GET    <platform api>/voice/takes/<id>/audio                            → the audio

var voiceTakesPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/voice/takes(?:/([A-Za-z0-9_.-]{1,64})(/audio)?)?$`)

const (
	// voiceAudioKept is how long the audio of a take is kept; past it only its text is left.
	voiceAudioKept = 30 * 24 * time.Hour
	// maxVoiceAudio is the largest recording kept: some ten minutes of compressed speech.
	maxVoiceAudio = 8 << 20
	// maxVoiceTakes bounds what one person keeps; the oldest go first.
	maxVoiceTakes = 500
	maxVoiceText  = 100_000
)

var (
	voiceMime  = regexp.MustCompile(`^audio/[A-Za-z0-9.+-]{1,40}$`)
	voiceWord  = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)
	voiceState = map[string]bool{"failed": true, "done": true}
)

type voiceTake struct {
	ID        string  `json:"id"`
	MachineID string  `json:"machineId"`
	At        int64   `json:"at"`
	Ms        int64   `json:"ms"`
	Mime      string  `json:"mime"`
	State     string  `json:"state"`
	Text      *string `json:"text"`
	Audio     bool    `json:"audio"`
}

const voiceCols = `id, machine_id, at, ms, mime, state, text, audio IS NOT NULL`

func scanVoiceTake(sc interface{ Scan(...any) error }) (voiceTake, error) {
	var t voiceTake
	var text sql.NullString
	if err := sc.Scan(&t.ID, &t.MachineID, &t.At, &t.Ms, &t.Mime, &t.State, &text, &t.Audio); err != nil {
		return t, err
	}
	if text.Valid {
		t.Text = &text.String
	}
	return t, nil
}

func (s *Service) serveVoiceTakes(w http.ResponseWriter, r *http.Request, p *auth.Principal, rest string) {
	m := voiceTakesPath.FindStringSubmatch(rest)
	id, audio := m[1], m[2] != ""
	ctx, user := r.Context(), p.User.ID
	switch {
	case id == "" && r.Method == http.MethodGet:
		s.voiceExpire(ctx, user)
		rows, err := s.db.Query(ctx, `SELECT `+voiceCols+` FROM voice_takes WHERE user_id = ? ORDER BY at DESC, id`, user)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		defer rows.Close()
		takes := []voiceTake{}
		for rows.Next() {
			t, err := scanVoiceTake(rows)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			takes = append(takes, t)
		}
		if err := rows.Err(); err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"takes": takes})
	case id != "" && audio && r.Method == http.MethodGet:
		var enc, mime sql.NullString
		err := s.db.QueryRow(ctx, `SELECT audio, mime FROM voice_takes WHERE user_id = ? AND id = ?`, user, id).Scan(&enc, &mime)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !enc.Valid) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		raw, err := base64.StdEncoding.DecodeString(enc.String)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", mime.String)
		w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(raw)
	case id != "" && !audio && r.Method == http.MethodPut:
		s.putVoiceTake(w, r, user, id)
	case id != "" && !audio && r.Method == http.MethodPatch:
		s.patchVoiceTake(w, r, user, id)
	case id != "" && !audio && r.Method == http.MethodDelete:
		if _, err := s.db.Exec(ctx, `DELETE FROM voice_takes WHERE user_id = ? AND id = ?`, user, id); err != nil {
			s.fail(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method"})
	}
}

// voiceExpire empties the audio of the person's takes past voiceAudioKept; their text stays.
func (s *Service) voiceExpire(ctx context.Context, user string) {
	if _, err := s.db.Exec(ctx, `UPDATE voice_takes SET audio = NULL WHERE user_id = ? AND audio IS NOT NULL AND at < ?`,
		user, store.Ms(s.now().Add(-voiceAudioKept))); err != nil && ctx.Err() == nil {
		s.log.Warn("voice takes: old audio not removed", "err", err)
	}
}

func voiceBad(w http.ResponseWriter, msg string) {
	httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

func (s *Service) putVoiceTake(w http.ResponseWriter, r *http.Request, user, id string) {
	ctx, q := r.Context(), r.URL.Query()
	at, err1 := strconv.ParseInt(q.Get("at"), 10, 64)
	ms, err2 := strconv.ParseInt(q.Get("ms"), 10, 64)
	state := q.Get("state")
	machine := q.Get("machine")
	if err1 != nil || err2 != nil || at <= 0 || ms < 0 || !voiceState[state] || !voiceWord.MatchString(machine) {
		voiceBad(w, "Нужны время, длина и состояние записи")
		return
	}
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if !voiceMime.MatchString(mime) {
		mime = "audio/webm"
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVoiceAudio))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		httpx.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "Запись слишком длинная"})
		return
	}
	if err != nil {
		voiceBad(w, "Запись не прочитана")
		return
	}
	// An empty body keeps a take that has no audio (the text alone).
	var enc any
	if len(raw) > 0 {
		enc = base64.StdEncoding.EncodeToString(raw)
	}
	now := store.Ms(s.now())
	if _, err := s.db.Exec(ctx, `INSERT INTO voice_takes (user_id, id, machine_id, at, ms, mime, state, audio, created_at, changed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, id) DO UPDATE SET machine_id = excluded.machine_id, at = excluded.at, ms = excluded.ms,
			mime = excluded.mime, state = excluded.state, audio = excluded.audio, changed_at = excluded.changed_at`,
		user, id, machine, at, ms, mime, state, enc, now, now); err != nil {
		s.fail(w, r, err)
		return
	}
	// The oldest go past the bound.
	if _, err := s.db.Exec(ctx, `DELETE FROM voice_takes WHERE user_id = ? AND id NOT IN (
		SELECT id FROM voice_takes WHERE user_id = ? ORDER BY at DESC, id LIMIT ?)`, user, user, maxVoiceTakes); err != nil {
		s.log.Warn("voice takes: old takes not removed", "err", err)
	}
	t, err := scanVoiceTake(s.db.QueryRow(ctx, `SELECT `+voiceCols+` FROM voice_takes WHERE user_id = ? AND id = ?`, user, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"take": t})
}

func (s *Service) patchVoiceTake(w http.ResponseWriter, r *http.Request, user, id string) {
	ctx := r.Context()
	var patch struct {
		Text  json.RawMessage `json:"text"`
		State *string         `json:"state"`
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVoiceText*4+1024))
	if err != nil || json.Unmarshal(raw, &patch) != nil {
		voiceBad(w, "Нужен JSON")
		return
	}
	set, args := []string{"changed_at = ?"}, []any{store.Ms(s.now())}
	if patch.Text != nil {
		var text *string
		if json.Unmarshal(patch.Text, &text) != nil || (text != nil && utf8.RuneCountInString(*text) > maxVoiceText) {
			voiceBad(w, "Текст записи не тот")
			return
		}
		set, args = append(set, "text = ?"), append(args, text)
	}
	if patch.State != nil {
		if !voiceState[*patch.State] {
			voiceBad(w, "Состояние записи не то")
			return
		}
		set, args = append(set, "state = ?"), append(args, *patch.State)
	}
	res, err := s.db.Exec(ctx, `UPDATE voice_takes SET `+strings.Join(set, ", ")+` WHERE user_id = ? AND id = ?`, append(args, user, id)...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	t, err := scanVoiceTake(s.db.QueryRow(ctx, `SELECT `+voiceCols+` FROM voice_takes WHERE user_id = ? AND id = ?`, user, id))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"take": t})
}
