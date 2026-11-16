package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatgpt"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// The ChatGPT integration (internal/chatgpt): a plain conversation with ChatGPT on the person's own plan,
// signed in with «Sign in with ChatGPT». It is the person's alone: only their page (a page session, same origin) gets
// in, never an agent's cli-token, and its tokens stay in this machine's own safe file whatever server safe the runtime
// uses. Nothing of it goes to the hub.
//
//	GET    <platform>/chatgpt                         {accounts, pending}
//	POST   <platform>/chatgpt/login                   {account?} -> {url, redirect}
//	POST   <platform>/chatgpt/login/callback          {url} -> {account}
//	DELETE <platform>/chatgpt/login
//	POST   <platform>/chatgpt/accounts/<key>/logout   -> {revoked}
//	POST   <platform>/chatgpt/accounts/<key>/active
//	DELETE <platform>/chatgpt/accounts/<key>          -> {revoked}
//	GET    <platform>/chatgpt/models                  {models}
//	GET    <platform>/chatgpt/chats                   {chats}
//	POST   <platform>/chatgpt/chats                   {model, effort} -> {chat}
//	GET    <platform>/chatgpt/chats/<id>              {chat, running}
//	PATCH  <platform>/chatgpt/chats/<id>              {title}
//	DELETE <platform>/chatgpt/chats/<id>
//	POST   <platform>/chatgpt/chats/<id>/messages     {text, model, effort, files: [{name, mime, data}]} -> {message}
//	GET    <platform>/chatgpt/chats/<id>/stream       text/event-stream: delta {text}, done {message}, idle
//	POST   <platform>/chatgpt/chats/<id>/spoken            {lines: [{role, text}]} -> {messages}  (voice mode)
//	POST   <platform>/chatgpt/chats/<id>/stop
//	GET    <platform>/chatgpt/chats/<id>/files/<file>

var (
	chatgptMu  sync.Mutex
	chatgptFor = map[string]*chatgpt.Service{}
)

// chatgptService is the one integration of a data folder.
func chatgptService(d deps) *chatgpt.Service {
	chatgptMu.Lock()
	defer chatgptMu.Unlock()
	if s := chatgptFor[d.DataDir]; s != nil {
		return s
	}
	s := chatgpt.New(chatgpt.Config{
		Dir:       filepath.Join(d.DataDir, "chatgpt"),
		Vault:     storeFor(d.DataDir, d.Home),
		Endpoints: chatgptEndpoints(os.Getenv),
	})
	chatgptFor[d.DataDir] = s
	return s
}

// chatgptEndpoints are OpenAI's own, or a test's fake on this machine named by RUNTIME_CHATGPT_AUTH_URL and
// RUNTIME_CHATGPT_API_URL (loopback only: a token never goes to another host).
func chatgptEndpoints(getenv func(string) string) chatgpt.Endpoints {
	ep := chatgpt.DefaultEndpoints()
	auth, api := strings.TrimSpace(getenv("RUNTIME_CHATGPT_AUTH_URL")), strings.TrimSpace(getenv("RUNTIME_CHATGPT_API_URL"))
	if auth == "" && api == "" {
		return ep
	}
	if !chatgptEndpointOK(auth) || !chatgptEndpointOK(api) {
		agent.Log("warn", "chatgpt: RUNTIME_CHATGPT_AUTH_URL and RUNTIME_CHATGPT_API_URL must both be loopback addresses; OpenAI's own are used")
		return ep
	}
	return chatgpt.Endpoints{Auth: auth, API: api}
}

func chatgptEndpointOK(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return (u.Scheme == "http" || u.Scheme == "https") && ip != nil && ip.IsLoopback()
}

func chatgptRoutes(d deps) []gate.Route {
	guard := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	var handlers []gate.Route
	for _, base := range []string{platformAPI + "/chatgpt", "/agents" + platformAPI + "/chatgpt"} {
		var h http.Handler
		var once sync.Once
		handlers = append(handlers, gate.Route{
			Match: func(r *http.Request) bool { return r.URL.Path == base || strings.HasPrefix(r.URL.Path, base+"/") },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() { h = chatgptService(d).Handler(base, guard) })
				h.ServeHTTP(w, r)
			}),
		})
	}
	return handlers
}
