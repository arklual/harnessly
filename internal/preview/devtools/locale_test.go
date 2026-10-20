package devtools

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishQueryAndSettingsErrors(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	if _, msg := ParseSettingsPatch("x"); msg != "A settings object is required" {
		t.Errorf("settings: %q", msg)
	}
	if _, msg := ParseNetworkQuery(Query{"since": {"-1"}}); msg != "since: an integer from 0, the cursor of the previous answer" {
		t.Errorf("since: %q", msg)
	}
	if _, msg := ParseNetworkQuery(Query{"limit": {"0"}}); !strings.HasPrefix(msg, "limit: an integer from 1 to ") {
		t.Errorf("limit: %q", msg)
	}
	if _, msg := ParseNetworkQuery(Query{"type": {"nonsense"}}); !strings.HasSuffix(msg, "; several separated by commas") {
		t.Errorf("type: %q", msg)
	}
	if _, _, msg := ParseServerLogQuery(Query{"lines": {"x"}}); !strings.HasPrefix(msg, "lines: an integer from 1 to ") {
		t.Errorf("lines: %q", msg)
	}
	api := &API{CLI: "acme"}
	if got := api.noSlotMessage(); got != "The preview has not been opened yet: open the page in the pane browser: acme panel open browser <port>" {
		t.Errorf("no slot: %q", got)
	}
	t.Setenv("RUNTIME_LOCALE", "ru")
	if _, msg := ParseNetworkQuery(Query{"since": {"-1"}}); msg != "since: целое число от 0, курсор прошлого ответа" {
		t.Errorf("ru since: %q", msg)
	}
}
