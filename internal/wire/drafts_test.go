package wire

import (
	"strings"
	"testing"
)

func TestDraftCommandsAreChecked(t *testing.T) {
	bad := []string{
		`{"opId":"o","key":"chat-1","text":"x","rev":1}`,
		`{"opId":"o","key":"new","text":"x","rev":0}`,
		`{"opId":"","key":"new","text":"x","rev":1}`,
		`{"opId":"o","key":"new","text":"` + strings.Repeat("я", DraftTextMax+1) + `","rev":1}`,
		`{"opId":"o","key":"new","text":"x","rev":1,"deviceId":"d"}`,
	}
	for _, body := range bad {
		if _, err := DecodeCommand(OpDraftSet, []byte(body)); err == nil {
			t.Errorf("accepted %.80s", body)
		}
	}
	for _, body := range []string{
		`{"opId":"o","key":"new","text":"","rev":2}`,
		`{"opId":"o","key":"tab-k3Jx9QmZ","text":"` + strings.Repeat("я", DraftTextMax) + `","rev":1}`,
	} {
		if _, err := DecodeCommand(OpDraftSet, []byte(body)); err != nil {
			t.Errorf("a good draft %.60s: %v", body, err)
		}
	}
}

func TestADraftKeyIsAConversationOrTheNewChat(t *testing.T) {
	for key, ok := range map[string]bool{"new": true, "tab-a1": true, "": false, "New": false, "tab-": false, "site:x": false} {
		if ValidDraftKey(key) != ok {
			t.Errorf("%q: %v", key, !ok)
		}
	}
}
