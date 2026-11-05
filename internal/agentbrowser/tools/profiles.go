package tools

import (
	"context"
	"encoding/json"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func (t Tools) profileCall(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	for _, key := range args.Keys() {
		expected := name == "chrome_use_profile" && key == "name" || name == "chrome_request_profile" && key == "why"
		if !expected {
			return mcpserver.TextResult("Unexpected profile argument", true)
		}
		value, _ := args.Get(key)
		if _, ok := value.(string); !ok {
			return mcpserver.TextResult("Profile arguments must be strings", true)
		}
	}
	a := t.S.Access
	if a == nil {
		return mcpserver.TextResult("Browser profile access is unavailable", true)
	}
	var result any
	var err error
	switch name {
	case "chrome_profiles":
		profiles, current, e := a.List(t.Caller)
		err = e
		result = map[string]any{"profiles": profiles, "current": current}
	case "chrome_use_profile":
		p, e := a.UseName(ctx, t.Caller, args.String("name"))
		err = e
		result = map[string]any{"profile": p, "message": "Profile selected; this chat's tabs in the previous profile were closed."}
	case "chrome_request_profile":
		result, err = a.Request(ctx, t.Caller, args.String("why"))
	}
	if err != nil {
		return mcpserver.TextResult(err.Error(), true)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return mcpserver.TextResult("Cannot encode profile result", true)
	}
	return mcpserver.TextResult(string(raw), false)
}
