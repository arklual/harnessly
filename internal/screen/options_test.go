package screen

import (
	"reflect"
	"strings"
	"testing"
)

func lines(l ...string) string { return strings.Join(l, "\n") }

func TestParsePermissionOptions(t *testing.T) {
	cases := []struct {
		name string
		pane string
		want Options
	}{
		{
			name: "numbered Yes/No with the focus on the first option",
			pane: lines("Do you want to proceed?", "", "❯ 1. Yes", "  2. No"),
			want: Options{Options: []string{"1. Yes", "2. No"}, Request: "Do you want to proceed?"},
		},
		{
			name: `a narrow pane wraps "2. Yes" into "2Yes"`,
			pane: lines("Permission required", "", "  1. Yes", "❯ 2Yes, and don't ask again", "  3. No"),
			want: Options{Options: []string{"1. Yes", "2. Yes, and don't ask again", "3. No"}, FocusedIndex: 1, Request: "Permission required"},
		},
		{
			name: "an unknown pattern gives nothing",
			pane: lines("Some random output", "❯ Foo", "  Bar"),
			want: Options{Options: []string{}},
		},
		{
			name: "a damaged capture gets its option labels back",
			pane: lines(
				"Do you want to proceed?",
				" ❯ 1. Yescurrent status for this tab",
				"  2.Yes, and don't ask: curl -s http://localhost:8022/api/status",
				"",
				"   3. No",
				" Esc to cancel · Tab to amend · ctrl+e to explain",
			),
			want: Options{
				Options:      []string{"1. Yes", "2. Yes, and don't ask again for: curl -s http://localhost:8022/api/status", "3. No"},
				FocusedIndex: 0,
				Request:      "Do you want to proceed?",
			},
		},
		{
			name: "a long option soft-wrapped onto the next line is joined",
			pane: lines(
				" Do you want to proceed?",
				" ❯ 1. Yes",
				"   2. Yes, and don’t ask again for: python3 -c \"import sys,json; d=json.loads(sys.stdin.read()); print('type:',d.get('type'),'| timestamp:',d.get('timestamp'),'|",
				"                                  stop_reason:',d.get('message',{}).get('stop_reason'))\"",
				"   3. No",
			),
			want: Options{
				Options: []string{
					"1. Yes",
					"2. Yes, and don’t ask again for: python3 -c \"import sys,json; d=json.loads(sys.stdin.read()); print('type:',d.get('type'),'| timestamp:',d.get('timestamp'),'|stop_reason:',d.get('message',{}).get('stop_reason'))\"",
					"3. No",
				},
				Request: "Do you want to proceed?",
			},
		},
		{
			name: "an earlier prompt left in scrollback: the newest block wins",
			pane: lines(
				"Do you want to proceed?",
				" ❯ 1. Yes",
				"   2. Yes, and don’t ask again for: python3 -c \"import sys,json\"",
				"   3. No",
				"",
				"> running command...",
				"output line",
				"",
				"Do you want to proceed?",
				"   1. Yes",
				" ❯ 2. Yes, and don’t ask again for tmux list-sessions",
				"   3. No",
			),
			want: Options{
				Options:      []string{"1. Yes", "2. Yes, and don’t ask again for tmux list-sessions", "3. No"},
				FocusedIndex: 1,
				Request:      "Do you want to proceed?",
			},
		},
		{
			name: "keyword prompt Accept/Decline",
			pane: lines("Trust this workspace?", "", "❯ Accept", "  Decline"),
			want: Options{Options: []string{"Accept", "Decline"}, Request: "Trust this workspace?"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParsePermissionOptions(c.pane); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v\nwant %#v", got, c.want)
			}
		})
	}
}

func TestHasPermissionPrompt(t *testing.T) {
	if !HasPermissionPrompt("\n❯ 1. Yes\n  2. No\n") {
		t.Error("a valid prompt is not seen")
	}
	if HasPermissionPrompt("just some logs\nmore logs") {
		t.Error("logs read as a prompt")
	}
}

func TestParseChoiceOptions(t *testing.T) {
	cases := []struct {
		name       string
		pane       string
		want       Options
		permission bool
	}{
		{
			name: "AskUserQuestion numbered menu is a plain choice, not a permission prompt",
			pane: lines(
				" ☐ 변경사항",
				"",
				"커밋 안 된 변경사항(pane-container.tsx, capture-at-width.ts)을 어떻게 처리할까요?",
				"",
				"❯ 1. 내용 먼저 리뷰",
				"     두 파일의 diff를 분석해서 무엇이 바뀌었는지, 문제는 없는지 설명합니다.",
				"  2. 바로 커밋",
				"     변경사항을 영어 커밋 메시지로 정리해 커밋합니다.",
				"  3. 그대로 두기",
				"     아무것도 하지 않고 다른 작업을 진행합니다.",
				"  4. Type something.",
				"────────────────────────────────────────────────────────────────",
				"  5. Chat about this",
			),
			want: Options{
				Options:      []string{"1. 내용 먼저 리뷰", "2. 바로 커밋", "3. 그대로 두기", "4. Type something.", "5. Chat about this"},
				FocusedIndex: 0,
				Title:        "변경사항",
				Request:      "커밋 안 된 변경사항(pane-container.tsx, capture-at-width.ts)을 어떻게 처리할까요?",
			},
		},
		{
			name: "AskUserQuestion title and question above the options",
			pane: lines(
				" ☐ 테마",
				"",
				"어떤 색상 테마를 선택하시겠어요?",
				"",
				"❯ 1. 다크 모드",
				"     어두운 배경에 밝은 텍스트. 야간 작업에 적합합니다.",
				"  2. 라이트 모드",
				"     밝은 배경에 어두운 텍스트. 주간 작업에 적합합니다.",
				"  3. 시스템 설정",
				"     OS의 다크/라이트 설정을 자동으로 따라갑니다.",
				"  4. Type something.",
				"────────────────────────────────────────────────────────────────",
				"  5. Chat about this",
				"",
				"Enter to select · ↑/↓ to navigate · Esc to cancel",
			),
			want: Options{
				Options:      []string{"1. 다크 모드", "2. 라이트 모드", "3. 시스템 설정", "4. Type something.", "5. Chat about this"},
				FocusedIndex: 0,
				Title:        "테마",
				Request:      "어떤 색상 테마를 선택하시겠어요?",
			},
		},
		{
			name: "numbers hidden by scrolling keep the drawn prefixes",
			pane: lines("❯ 1. 첫 번째", "  2. 두 번째", "↓ 4. 네 번째", "  6. Chat about this"),
			want: Options{Options: []string{"1. 첫 번째", "2. 두 번째", "4. 네 번째", "6. Chat about this"}},
		},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseChoiceOptions(c.pane); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v\nwant %#v", got, c.want)
			}
			if i == 0 {
				if got := ParsePermissionOptions(c.pane); !reflect.DeepEqual(got, Options{Options: []string{}}) {
					t.Errorf("permission: got %#v", got)
				}
			}
		})
	}
}

func TestReadPromptOptions(t *testing.T) {
	// The permission-options route: a permission prompt first, any menu otherwise, and Claude's bypass warning flagged.
	trust := ReadPromptOptions(fixture(t, "claude-trust-folder.txt"))
	if len(trust.Options.Options) != 2 || trust.BypassPrompt {
		t.Errorf("trust folder: %#v", trust)
	}
	menu := ReadPromptOptions(fixture(t, "codex-model-step2.txt"))
	if len(menu.Options.Options) < 4 || menu.FocusedIndex != 2 {
		t.Errorf("codex step 2: %#v", menu)
	}
	bypass := ReadPromptOptions(lines("WARNING: Bypass Permissions mode", "", "❯ 1. No, exit", "  2. Yes, I accept"))
	if !bypass.BypassPrompt || len(bypass.Options.Options) != 2 {
		t.Errorf("bypass: %#v", bypass)
	}
}

func TestParsePlanOptions(t *testing.T) {
	pane := lines(
		"Would you like to proceed?",
		"",
		"❯ 1. Yes, and auto-accept edits",
		"  2. Yes, and manually approve edits",
		"  3. No, keep planning",
		"",
		"  ctrl+g to edit in your editor",
	)
	want := []string{"Yes, and auto-accept edits", "Yes, and manually approve edits", "No, keep planning"}
	if got := ParsePlanOptions(pane); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	if got := ParsePlanOptions("plain output"); len(got) != 0 {
		t.Errorf("got %q", got)
	}
}

func TestOptionNumber(t *testing.T) {
	if n, ok := OptionNumber("12. Twelve"); !ok || n != "12" {
		t.Errorf("got %q %v", n, ok)
	}
	if _, ok := OptionNumber("Accept"); ok {
		t.Error("a label without a number has one")
	}
	if got := StripNumberPrefix("2. No, go back"); got != "No, go back" {
		t.Errorf("got %q", got)
	}
}
