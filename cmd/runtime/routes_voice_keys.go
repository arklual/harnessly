package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/providers"
	"github.com/ly-solutions/harnessly/internal/voice"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The keys of the cloud recognition models, beside the keys of the agents' vendors (Settings → «Модели и ключи»):
//
//	GET    <platform>/voice/keys           {keys: {openai, yandex, yandex-folder: {configured, tail}}, chatgpt: bool}
//	PUT    <platform>/voice/keys/<id>      {key}   the same list after it, or 400 {error}
//	DELETE <platform>/voice/keys/<id>              the same list after it
//
// They are kept as the vendors' keys are (providers.Store: <data>/providers/<id>.key, mode 0600) and never leave the
// machine but in the request to the provider the person picked as the recognition model.

const voiceKeysPath = "/voice/keys"

// sharedStores are the key stores the runtime opened, by data dir. The speech keys go through the very store whose masks
// the wire redactor holds, so a key saved on the page is masked in what an agent prints the moment it is saved.
var sharedStores sync.Map

// shareProviders makes st the store of dataDir's keys for the dictation as well.
func shareProviders(dataDir string, st *providers.Store) {
	sharedStores.Store(dataDir, st)
}

// voiceStore is the store of dataDir's keys: the runtime's own when it is up, else one on the same directory.
func voiceStore(dataDir string) *providers.Store {
	if st, ok := sharedStores.Load(dataDir); ok {
		return st.(*providers.Store)
	}
	st, _ := sharedStores.LoadOrStore(dataDir, &providers.Store{Dir: filepath.Join(dataDir, "providers")})
	return st.(*providers.Store)
}

// voiceCredentials gives the dictation the saved key (and folder) of a cloud engine.
func voiceCredentials(dataDir string) func(engine string) (string, string, error) {
	return func(engine string) (string, string, error) {
		st := voiceStore(dataDir)
		switch engine {
		case "openai":
			key, err := st.Key(providers.SpeechOpenAI)
			return key, "", err
		case "yandex":
			key, err := st.Key(providers.SpeechYandex)
			if err != nil {
				return "", "", err
			}
			folder, err := st.Key(providers.SpeechYandexFolder)
			if err != nil && !errors.Is(err, providers.ErrNoKey) {
				return "", "", err
			}
			return key, folder, nil
		case "chatgpt":
			login := voiceCodexLogin(dataDir)
			if login == nil {
				return "", "", providers.ErrNoLogin
			}
			token, err := login.Token(context.Background())
			if err != nil {
				return "", "", err
			}
			return token, login.AccountID(), nil
		}
		return "", "", errors.New("not a cloud engine")
	}
}

// voiceCodexLogin is the Codex CLI's sign-in by a ChatGPT plan: the store's when the runtime keeps one, so that a renewal
// is not run twice at once, else one on the same file; nil without a home.
func voiceCodexLogin(dataDir string) *providers.CodexLogin {
	if login, ok := voiceStore(dataDir).Logins["gpt"].(*providers.CodexLogin); ok && login != nil {
		return login
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return &providers.CodexLogin{Path: filepath.Join(providers.CodexHome(home), "auth.json")}
}

// voiceChatGPTReady says the machine's Codex is signed in by a plan, so the dictation can be offered it.
func voiceChatGPTReady(dataDir string) bool {
	login := voiceCodexLogin(dataDir)
	if login == nil {
		return false
	}
	_, ok := login.Account()
	return ok
}

func voiceKeysRoutes(d deps, _ *voice.Server) []gate.Route {
	list := func() map[string]any {
		st := voiceStore(d.DataDir)
		keys := map[string]wire.ProfileKey{}
		for _, id := range providers.SpeechKeys {
			keys[id] = st.State(id)
		}
		return map[string]any{"keys": keys, "chatgpt": voiceChatGPTReady(d.DataDir)}
	}
	return []gate.Route{{
		Slice: "voice",
		Match: func(r *http.Request) bool {
			return pathIs(r, platformAPI+voiceKeysPath) || strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), platformAPI+voiceKeysPath+"/")
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !d.SessionOK(r) {
				peerJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
				return
			}
			id := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), platformAPI+voiceKeysPath), "/")
			switch {
			case id == "" && r.Method == http.MethodGet:
				peerJSON(w, http.StatusOK, list())
			case id != "" && !providers.IsSpeechKey(id):
				peerJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
			case id != "" && r.Method == http.MethodPut:
				var body wire.ProfileKeyEdit
				raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, wire.MaxProfileKeyBody))
				if err == nil {
					err = json.Unmarshal(raw, &body)
				}
				if err == nil {
					_, err = voiceStore(d.DataDir).Set(id, body.Key)
				}
				if err != nil {
					peerJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				peerJSON(w, http.StatusOK, list())
			case id != "" && r.Method == http.MethodDelete:
				if _, err := voiceStore(d.DataDir).Remove(id); err != nil {
					peerJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				peerJSON(w, http.StatusOK, list())
			default:
				w.Header().Set("Allow", "GET, PUT, DELETE")
				peerJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			}
		}),
	}}
}
