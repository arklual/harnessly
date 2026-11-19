package routinetools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestCreateSendsTheRoutine(t *testing.T) {
	var got hubproto.RoutinesCall
	tools := Tools{Chat: "tab-1", Hub: func(_ context.Context, c hubproto.RoutinesCall) (int, []byte, error) {
		got = c
		return 200, []byte(`{"routine":{"id":"r1","name":"Дайджест","enabled":true},"next":["2026-11-19T06:00:00Z"]}`), nil
	}}
	res := tools.Call(context.Background(), ToolCreate, jsvalue.NewObject("name", "Дайджест", "prompt", "собери", "cron", "0 9 * * 1-5",
		"channels", []any{"telegram", "file"}, "file", "digest/{date}.md", "checks", []any{jsvalue.NewObject("kind", "file")}))
	out := jsvalue.Stringify(res, "")
	if !strings.Contains(string(out), "made: Дайджест") || !strings.Contains(string(out), "next start") {
		t.Fatalf("answer: %s", out)
	}
	var body map[string]any
	_ = json.Unmarshal(got.Body, &body)
	rt := body["routine"].(map[string]any)
	d := rt["delivery"].(map[string]any)
	if got.Op != "create" || rt["chatId"] != "tab-1" || d["file"] != "digest/{date}.md" || len(d["channels"].([]any)) != 2 {
		t.Fatalf("body: %s", got.Body)
	}
	if body["trigger"].(map[string]any)["schedule"].(map[string]any)["cron"] != "0 9 * * 1-5" {
		t.Fatalf("trigger: %s", got.Body)
	}
}

func TestUpdateKeepsWhatIsNotNamed(t *testing.T) {
	var calls []hubproto.RoutinesCall
	tools := Tools{Hub: func(_ context.Context, c hubproto.RoutinesCall) (int, []byte, error) {
		calls = append(calls, c)
		if c.Op == "get" {
			return 200, []byte(`{"routine":{"id":"r1","routine":{"words":"по будням","delivery":{"when":"changed","channels":["telegram"]}}}}`), nil
		}
		return 200, []byte(`{"routine":{"id":"r1","name":"x","enabled":false}}`), nil
	}}
	tools.Call(context.Background(), ToolUpdate, jsvalue.NewObject("id", "r1", "when", "failure"))
	var body map[string]any
	_ = json.Unmarshal(calls[1].Body, &body)
	rt := body["routine"].(map[string]any)
	d := rt["delivery"].(map[string]any)
	if rt["words"] != "по будням" || d["when"] != "failure" || d["channels"].([]any)[0] != "telegram" {
		t.Fatalf("update lost fields: %s", calls[1].Body)
	}
	calls = nil
	tools.Call(context.Background(), ToolUpdate, jsvalue.NewObject("id", "r1", "enabled", false))
	body = map[string]any{}
	_ = json.Unmarshal(calls[1].Body, &body)
	if _, has := body["routine"]; has || body["enabled"] != false {
		t.Fatalf("pause body: %s", calls[1].Body)
	}
}

func TestRefusalIsSaidInWords(t *testing.T) {
	tools := Tools{Hub: func(context.Context, hubproto.RoutinesCall) (int, []byte, error) {
		return 400, []byte(`{"error":"invalid_request","message":"Имя правила: от 1 до 120 символов"}`), nil
	}}
	out := jsvalue.Stringify(tools.Call(context.Background(), ToolList, jsvalue.NewObject()), "")
	if !strings.Contains(string(out), "Имя правила") {
		t.Fatalf("refusal: %s", out)
	}
}
