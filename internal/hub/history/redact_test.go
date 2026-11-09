package history

import (
	"encoding/json"
	"strings"
	"testing"
)

// The cases of the gateway's own test (its redact-line test): the hub cuts what the
// cluster cuts, into the same placeholders.
func TestSecretsAreCutAsTheGatewayCutsThem(t *testing.T) {
	jwt := strings.Join([]string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiJ4eHh4eHh4eHgifQ", "c2lnbmF0dXJlc2lnbmF0dXJl"}, ".")

	plain := json.RawMessage(`{"type":"entry_append","entry":{"t":"assistant","text":"готово <b>&</b>"}}`)
	if out, found := RedactSecrets(plain); string(out) != string(plain) || len(found) != 0 {
		t.Errorf("an ordinary event changed: %s %v", out, found)
	}

	command := `curl -H "Authorization: Bearer ` + strings.Repeat("A", 12) + strings.Repeat("b9", 10) + `" -H "X: ` + jwt + `"`
	raw, _ := json.Marshal(map[string]any{"entry": map[string]any{"input": map[string]any{"command": command}}})
	out, found := RedactSecrets(raw)
	var doc struct {
		Entry struct {
			Input struct{ Command string }
		}
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON any more: %s", out)
	}
	got := doc.Entry.Input.Command
	if !strings.Contains(got, "Bearer [скрыто: bearer]") || !strings.Contains(got, "[скрыто: jwt]") || strings.Contains(got, jwt) {
		t.Errorf("command %q", got)
	}
	if found["jwt"] != 1 || found["bearer"] != 1 || len(found) != 2 {
		t.Errorf("found %v", found)
	}

	text := "perm:cm9vdA==.NDctMg==." + strings.Repeat("Xy9", 8) + " glpat-" + strings.Repeat("a1", 12) + " hvs." + strings.Repeat("Q", 30)
	raw, _ = json.Marshal(map[string]string{"text": text})
	out, _ = RedactSecrets(raw)
	if want := `{"text":"[скрыто: youtrack] [скрыто: gitlab] [скрыто: vault]"}`; string(out) != want {
		t.Errorf("token shapes: %s", out)
	}

	if _, found := RedactSecrets(json.RawMessage(`{"text":"Bearer [скрыто: bearer]"}`)); len(found) != 0 {
		t.Errorf("a placeholder cut twice: %v", found)
	}

	// The rules that keep a part keep it; a connection string and a URL keep their user, not their password.
	raw, _ = json.Marshal(map[string]string{"a": "postgres://app:s3cr3t-pass@db:5432/x", "b": "https://bot:hunter2@example.com/x",
		"c": "123456789:" + strings.Repeat("A", 35), "d": "AKIA" + strings.Repeat("Z", 16)})
	out, _ = RedactSecrets(raw)
	for _, want := range []string{"postgres://app:[скрыто: conn-string]@db", "https://bot:[скрыто: url-credentials]@example.com",
		`"c":"[скрыто: telegram]"`, `"d":"[скрыто: aws]"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("%s not in %s", want, out)
		}
	}
	// A longer run is not a Telegram token.
	raw, _ = json.Marshal(map[string]string{"c": "123456789:" + strings.Repeat("A", 36)})
	if _, found := RedactSecrets(raw); len(found) != 0 {
		t.Errorf("a longer run was cut: %v", found)
	}

	// An escape in the JSON hides nothing.
	escaped := json.RawMessage(`{"text":"\u0042earer ` + strings.Repeat("Z", 30) + `"}`)
	if out, found := RedactSecrets(escaped); found["bearer"] != 1 || strings.Contains(string(out), strings.Repeat("Z", 30)) {
		t.Errorf("an escaped token stayed: %s", out)
	}
}
