package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/extproviders"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// The external providers of the Settings screen (internal/extproviders). All of it is the person's page: a page
// session, same origin. No answer holds a key.
//
//	GET    <platform>/ext-providers                {providers: [View], presets: [Preset], accounts: true, launch: true}
//	POST   <platform>/ext-providers                {name, baseUrl, api, preset?, key?, models?} -> 201 {provider: View}
//	POST   <platform>/ext-providers/check          {baseUrl, api, key?, id?} -> Check (a form that is not saved yet)
//	PATCH  <platform>/ext-providers/<id>           {name?, baseUrl?, api?, key?, models?} -> {provider: View}
//	DELETE <platform>/ext-providers/<id>           {ok: true}
//	POST   <platform>/ext-providers/<id>/check     Check (the models it lists are kept)
//	GET    <platform>/ext-providers/<id>/models    {models, at, check?}; ?refresh=1 asks the provider again
//
// A refusal is {error: <reason>, message: <words for the person>}.

const extProviderBodyMax = 64 << 10

// extSecrets keeps the providers' keys in the machine's safe.
type extSecrets struct{ store secrets.Provider }

func (s extSecrets) Put(name, value string) error {
	_, _, err := s.store.Put(name, value)
	return err
}

func (s extSecrets) Reveal(name string) (string, error) {
	values, err := s.store.Reveal([]string{name}, secrets.Use{Title: locale.Pick("model provider", "провайдер моделей")})
	if err != nil {
		return "", err
	}
	return values[name], nil
}

func (s extSecrets) Remove(name string) error { return s.store.Remove(name) }
func (s extSecrets) Has(name string) bool     { return s.store.Has(name) }

var (
	extProvidersMu  sync.Mutex
	extProvidersAll = map[string]*extproviders.Service{}
)

// extProvidersFor is the process's one registry of a data folder: the routes and the background features share it.
func extProvidersFor(d deps) *extproviders.Service {
	extProvidersMu.Lock()
	defer extProvidersMu.Unlock()
	if s := extProvidersAll[d.DataDir]; s != nil {
		return s
	}
	dataDir := d.DataDir
	s := &extproviders.Service{
		Store:    &extproviders.Store{Dir: filepath.Join(dataDir, "v2", "ext-providers")},
		Secrets:  extSecrets{store: safeFor(d).store},
		Bound:    safeBound(safeFor(d).store),
		OnChange: tellExtProviderHearers(dataDir),
	}
	extProvidersAll[dataDir] = s
	return s
}

// tellExtProviderHearers is what a registry calls after a change: it tells the hearers of its data folder.
func tellExtProviderHearers(dataDir string) func(what string, before, after *extproviders.Provider) {
	return func(what string, before, after *extproviders.Provider) {
		extProvidersMu.Lock()
		hear := append([]func(string, *extproviders.Provider, *extproviders.Provider){}, extProviderHearers[dataDir]...)
		extProvidersMu.Unlock()
		for _, h := range hear {
			h(what, before, after)
		}
	}
}

// extProviderHearers are told of every change of a data folder's registry.
var extProviderHearers = map[string][]func(what string, before, after *extproviders.Provider){}

func onExtProviderChange(dataDir string, hear func(what string, before, after *extproviders.Provider)) {
	extProvidersMu.Lock()
	defer extProvidersMu.Unlock()
	extProviderHearers[dataDir] = append(extProviderHearers[dataDir], hear)
}

func extProviderMessage(reason string) string {
	switch reason {
	case extproviders.ReasonBadName:
		return locale.Pick("Give the provider a name of up to 60 characters.", "Дайте провайдеру название до 60 символов.")
	case extproviders.ReasonNameTaken:
		return locale.Pick("Another account is called so already. Give this one a name of its own.", "Так уже называется другой аккаунт. Дайте этому своё имя.")
	case extproviders.ReasonBadEnv:
		return locale.Pick("A variable needs a name of letters, digits and _ (once), a value of one line, and a secret one a value.", "У переменной имя из букв, цифр и _ (один раз), значение в одну строку, а у секретной оно обязательно.")
	case extproviders.ReasonEnvReserved:
		return locale.Pick("This variable is set by the launch itself and cannot be changed here.", "Эту переменную задаёт сам запуск, здесь её менять нельзя.")
	case extproviders.ReasonSubscriptionToken:
		return locale.Pick("This is the token of a Claude subscription. A machine has one sign-in by a subscription, in Settings → Claude Code; a provider takes a key of the API.", "Это токен подписки Claude. Вход по подписке на машине один — в настройках Claude Code; провайдеру нужен ключ API.")
	case extproviders.ReasonBadMediaKey:
		return locale.Pick("A Higgsfield key is two parts, the key ID and the secret, written as id:secret (console.higgsfield.ai).", "Ключ Higgsfield состоит из двух частей — ID и secret, пишутся через двоеточие: id:secret (console.higgsfield.ai).")
	case extproviders.ReasonBadURL:
		return locale.Pick("The address must start with https:// or http:// and name a host.", "Адрес должен начинаться с https:// или http:// и содержать имя сервера.")
	case extproviders.ReasonBadAPI:
		return locale.Pick("Choose the kind of API: OpenAI-compatible or Anthropic-compatible.", "Выберите тип API: совместимый с OpenAI или с Anthropic.")
	case extproviders.ReasonBadKey:
		return locale.Pick("The key does not look like one: it is too long or has line breaks.", "Ключ не похож на ключ: он слишком длинный или с переносами строк.")
	case extproviders.ReasonBadModel:
		return locale.Pick("A model's name must be one word without spaces.", "Название модели — одно слово без пробелов.")
	case extproviders.ReasonManaged:
		return locale.Pick("This provider comes from the team's environment. It is changed in the hub.", "Этот провайдер из окружения команды. Его меняют в хабе.")
	case extproviders.ReasonSecretUnbound:
		return locale.Pick("This secret is not allowed for this server. Ask the person to allow it: harnessly secret ask NAME --origin https://<host> --why …",
			"Этот секрет не разрешён для этого сервера. Попросите человека разрешить: harnessly secret ask ИМЯ --origin https://<хост> --why …")
	case extproviders.ReasonBadSecretRef:
		return locale.Pick("A secret is named by its name in the safe or by a vault: reference.", "Секрет называют именем в сейфе или ссылкой vault:.")
	case extproviders.ReasonSecretInSource:
		return locale.Pick("A token or a key is not written into the environment: name its secret instead.", "Токен или ключ в окружение не пишут: назовите секрет, в котором он лежит.")
	case extproviders.ReasonNotFound:
		return locale.Pick("There is no such provider any more.", "Такого провайдера уже нет.")
	case extproviders.ReasonTooMany:
		return locale.Pick("There are too many providers. Remove one you do not use.", "Провайдеров слишком много. Удалите ненужный.")
	}
	return locale.Pick("It did not work. Try again.", "Не получилось. Попробуйте ещё раз.")
}

func extProviderRefusal(w http.ResponseWriter, err error) {
	reason := extproviders.ReasonOf(err)
	status := http.StatusBadRequest
	switch reason {
	case extproviders.ReasonNotFound:
		status = http.StatusNotFound
	case extproviders.ReasonTooMany, extproviders.ReasonNameTaken, extproviders.ReasonManaged:
		status = http.StatusConflict
	case extproviders.ReasonFailed:
		status = http.StatusInternalServerError
		agent.Log("warn", "ext-providers: failed", agent.F{K: "error", V: err.Error()})
	}
	secretJSON(w, status, map[string]string{"error": reason, "message": extProviderMessage(reason)})
}

func extProviderRoutes(d deps) []gate.Route {
	svc := extProvidersFor(d)
	base := platformAPI + "/ext-providers"
	under := func(r *http.Request) (string, bool) {
		for _, prefix := range []string{base + "/", "/agents" + base + "/"} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && rest != "" {
				return rest, true
			}
		}
		return "", false
	}
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	read := func(w http.ResponseWriter, r *http.Request, out any) bool {
		raw, err := io.ReadAll(io.LimitReader(r.Body, extProviderBodyMax+1))
		if err != nil || len(raw) > extProviderBodyMax || json.Unmarshal(raw, out) != nil {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": extProviderMessage("")})
			return false
		}
		return true
	}
	notAllowed := func(w http.ResponseWriter) {
		secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}

	list := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			views, err := svc.List()
			if err != nil {
				extProviderRefusal(w, err)
				return
			}
			// accounts: this runtime keeps several accounts of one provider, tells them apart by name and marks the default.
			secretJSON(w, http.StatusOK, map[string]any{"providers": views, "presets": extproviders.Presets(), "accounts": true, "launch": true})
		case http.MethodPost:
			var in extproviders.Input
			if !read(w, r, &in) {
				return
			}
			view, err := svc.Create(in)
			if err != nil {
				extProviderRefusal(w, err)
				return
			}
			agent.Log("info", "ext-providers: added", agent.F{K: "id", V: view.ID}, agent.F{K: "host", V: view.Host})
			secretJSON(w, http.StatusCreated, map[string]any{"provider": view})
		default:
			notAllowed(w)
		}
	}

	one := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		rest, _ := under(r)
		parts := strings.Split(rest, "/")
		switch {
		case len(parts) == 1 && parts[0] == "check" && r.Method == http.MethodPost:
			var draft extproviders.Draft
			if !read(w, r, &draft) {
				return
			}
			got, err := svc.CheckDraft(r.Context(), draft)
			if err != nil {
				extProviderRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, got)
		case len(parts) == 1 && r.Method == http.MethodPatch:
			var patch extproviders.Patch
			if !read(w, r, &patch) {
				return
			}
			view, err := svc.Update(parts[0], patch)
			if err != nil {
				extProviderRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"provider": view})
		case len(parts) == 1 && r.Method == http.MethodDelete:
			if err := svc.Remove(parts[0]); err != nil {
				extProviderRefusal(w, err)
				return
			}
			agent.Log("info", "ext-providers: removed", agent.F{K: "id", V: parts[0]})
			secretJSON(w, http.StatusOK, map[string]bool{"ok": true})
		case len(parts) == 2 && parts[1] == "check" && r.Method == http.MethodPost:
			got, err := svc.Check(r.Context(), parts[0])
			if err != nil {
				extProviderRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, got)
		case len(parts) == 2 && parts[1] == "models" && r.Method == http.MethodGet:
			got, err := svc.Models(r.Context(), parts[0], r.URL.Query().Get("refresh") == "1")
			if err != nil {
				extProviderRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, got)
		default:
			notAllowed(w)
		}
	}

	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(list)},
		{Match: func(r *http.Request) bool { _, ok := under(r); return ok }, Handler: http.HandlerFunc(one)},
	}
}
