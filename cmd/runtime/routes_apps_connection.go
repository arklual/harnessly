package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/mcpconn"
)

// callConnectionTool is hl.tools.call for a server of an app that names a connection ("connection": "<catalog id>"):
// the call goes to the person's own server with the person's own credentials, which the connections' service keeps and
// refreshes (a token the server no longer takes is renewed once and the call repeated). The credentials stay in this
// function: the view gets the result, the app's package and the agent never see them. A person without the connection,
// or not signed in, leaves the app «ждёт вход»: the call is refused, not failed.
func callConnectionTool(ctx context.Context, ms *mcpconn.Service, granted bool, decl apps.MCPServer, tool string, args map[string]any) (json.RawMessage, error) {
	return connectionDo(ctx, ms, granted, decl, toolOp(tool, args))
}

func connectionDo(ctx context.Context, ms *mcpconn.Service, granted bool, decl apps.MCPServer, op mcpOp) (json.RawMessage, error) {
	if !granted {
		return nil, &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: apps.ToolRefused, Detail: "the app waits for a connection the person has not agreed to for it: " + decl.Connection}
	}
	signIn := &apps.SignIn{Connection: decl.Connection}
	conn, ok := ms.ByCatalog(decl.Connection)
	if !ok {
		return nil, &apps.BridgeError{Code: apps.CodeAuthRequired, Message: apps.AuthRequiredMessage, SignIn: signIn,
			Detail: "the person has no connection " + decl.Connection}
	}
	return signedInDo(ctx, ms, conn, signIn, op)
}

// callAppOAuthTool is hl.tools.call for an app's own server with auth oauth: the person signs in to the server itself,
// the way a connection does (the host keeps the server among the connections, disabled for agents, and its token in
// the safe). Not signed in yet: auth_required naming the app's server, which the host's «Войти» signs in to
// (POST <platform>/apps/signin).
func callAppOAuthTool(ctx context.Context, ms *mcpconn.Service, app, server string, _ apps.MCPServer, srv apps.MCPServer, tool string, args map[string]any) (json.RawMessage, error) {
	return appOAuthDo(ctx, ms, app, server, srv, toolOp(tool, args))
}

func appOAuthDo(ctx context.Context, ms *mcpconn.Service, app, server string, srv apps.MCPServer, op mcpOp) (json.RawMessage, error) {
	signIn := &apps.SignIn{App: app, Server: server}
	conn, ok := ms.ByOwner(appOwner(app, server))
	if !ok || conn.URL == "" || !sameServer(conn.URL, srv.URL) {
		return nil, &apps.BridgeError{Code: apps.CodeAuthRequired, Message: apps.AuthRequiredMessage, SignIn: signIn,
			Detail: "nobody signed in to the app's server yet"}
	}
	return signedInDo(ctx, ms, conn, signIn, op)
}

// signedInDo sends op to a server the connections' service signs in to: a token it no longer takes is renewed once
// and the request repeated; a refusal names where to sign in.
func signedInDo(ctx context.Context, ms *mcpconn.Service, conn mcpconn.Server, signIn *apps.SignIn, op mcpOp) (json.RawMessage, error) {
	if conn.Transport != mcpconn.HTTP {
		return nil, &apps.BridgeError{Code: apps.CodeHostNotAllowed, Message: apps.ToolRefused, Detail: "a view calls Streamable HTTP servers only"}
	}
	header, err := ms.Authorize(ctx, conn.ID)
	if err != nil {
		return nil, signInRefusal(err, signIn)
	}
	srv := apps.MCPServer{URL: conn.URL, Headers: flatHeaders(header), Credentialed: conn.Auth != mcpconn.AuthNone}
	result, err := op(ctx, srv)
	var be *apps.BridgeError
	if errors.As(err, &be) && be.Code == apps.CodeAuthExpired && conn.Auth != mcpconn.AuthNone {
		if header, err = ms.Reauthorize(ctx, conn.ID); err != nil {
			return nil, signInRefusal(err, signIn)
		}
		srv.Headers = flatHeaders(header)
		result, err = op(ctx, srv)
	}
	return result, withSignIn(err, signIn)
}

// appOwner is the owner of the sign-in made for one app's server (mcpconn.Server.Owner).
func appOwner(app, server string) string { return "app:" + app + "/" + server }

// sameServer says whether a kept sign-in is for the address the package names now: an update that moved the server
// elsewhere gets no token of the old one.
func sameServer(kept, declared string) bool {
	a, errA := url.Parse(kept)
	b, errB := url.Parse(declared)
	return errA == nil && errB == nil && strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host) &&
		strings.TrimSuffix(a.Path, "/") == strings.TrimSuffix(b.Path, "/")
}

// appSignInConnection is the sign-in an app's oauth server goes through: its own (Owner), never the person's other
// connection to the same address, made disabled for agents, so only this app's calls use it.
func appSignInConnection(ctx context.Context, ms *mcpconn.Service, app, server, appName string, srv apps.MCPServer) (mcpconn.Server, error) {
	if conn, ok := ms.ByOwner(appOwner(app, server)); ok && sameServer(conn.URL, srv.URL) {
		return conn, nil
	} else if ok {
		_ = ms.Remove(conn.ID) // the package moved the server: the old sign-in goes
	}
	conn, _, err := ms.Add(ctx, mcpconn.AddInput{URL: srv.URL, Name: appName, Owner: appOwner(app, server)})
	if err != nil {
		return mcpconn.Server{}, err
	}
	if conn.Auth != mcpconn.AuthOAuth {
		_ = ms.Remove(conn.ID)
		return mcpconn.Server{}, fmt.Errorf("%w: the app's server does not sign in by OAuth", mcpconn.ErrBadRequest)
	}
	return conn, nil
}

// signInRefusal is the code of a connection that gave no headers: no sign-in, an expired one, a sign-in service that
// did not answer, a refusal of the identity provider.
func signInRefusal(err error, signIn *apps.SignIn) error {
	detail := "the connection gave no sign-in: " + err.Error()
	switch {
	case errors.Is(err, mcpconn.ErrSignInExpired):
		return &apps.BridgeError{Code: apps.CodeAuthExpired, Message: apps.AuthExpiredMessage, SignIn: signIn, Detail: detail}
	case errors.Is(err, mcpconn.ErrSignInRequired):
		return &apps.BridgeError{Code: apps.CodeAuthRequired, Message: apps.AuthRequiredMessage, SignIn: signIn, Detail: detail}
	case errors.Is(err, mcpconn.ErrDenied), errors.Is(err, mcpconn.ErrOAuthFailed):
		return &apps.BridgeError{Code: apps.CodeAuthDenied, Message: apps.AuthDeniedMessage, Detail: detail}
	case errors.Is(err, mcpconn.ErrUnreachable):
		return &apps.BridgeError{Code: apps.CodeIdPUnreachable, Message: apps.IdPUnreachableMessage, Detail: detail}
	}
	return &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: apps.ToolRefused, Detail: detail}
}

// withSignIn names the connection on a refusal a new sign-in mends.
func withSignIn(err error, signIn *apps.SignIn) error {
	var be *apps.BridgeError
	if errors.As(err, &be) && (be.Code == apps.CodeAuthRequired || be.Code == apps.CodeAuthExpired) && be.SignIn == nil {
		be.SignIn = signIn
	}
	return err
}

func flatHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}
