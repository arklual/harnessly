package tools

import (
	"context"
	"encoding/json"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func (t Tools) loginCall(ctx context.Context, args *jsvalue.Object) *jsvalue.Object {
	for _, key := range args.Keys() {
		value, _ := args.Get(key)
		if key != "url" && key != "why" {
			return mcpserver.TextResult("Unexpected login argument", true)
		}
		if _, ok := value.(string); !ok {
			return mcpserver.TextResult("Login arguments must be strings", true)
		}
	}
	if t.S.Access == nil {
		return mcpserver.TextResult("Browser login is unavailable", true)
	}
	answer, err := t.S.Access.RequestLogin(ctx, t.Caller, args.String("url"), args.String("why"))
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		return mcpserver.TextResult("Cannot encode login result", true)
	}
	return mcpserver.TextResult(string(raw), false)
}
