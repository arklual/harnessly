package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
	conn, ok := ms.ByCatalog(decl.Connection)
	if !granted || !ok {
		return nil, &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: apps.ToolRefused, Detail: "the app waits for a connection (not made, not signed in, or not agreed to for this app): " + decl.Connection}
	}
	if conn.Transport != mcpconn.HTTP {
		return nil, &apps.BridgeError{Code: apps.CodeHostNotAllowed, Message: apps.ToolRefused, Detail: "a view calls Streamable HTTP servers only"}
	}
	header, err := ms.Authorize(ctx, conn.ID)
	if err != nil {
		return nil, signInRefusal(err)
	}
	result, err := apps.CallTool(ctx, apps.MCPServer{URL: conn.URL, Headers: flatHeaders(header)}, tool, args)
	var be *apps.BridgeError
	if errors.As(err, &be) && strings.Contains(be.Detail, "answered 401") && conn.Auth != mcpconn.AuthNone {
		if header, err = ms.Reauthorize(ctx, conn.ID); err != nil {
			return nil, signInRefusal(err)
		}
		return apps.CallTool(ctx, apps.MCPServer{URL: conn.URL, Headers: flatHeaders(header)}, tool, args)
	}
	return result, err
}

func signInRefusal(err error) error {
	return &apps.BridgeError{Code: apps.CodeHostUnavailable, Message: apps.ToolRefused, Detail: "the connection needs a sign-in: " + mcpconn.Reason(err)}
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
