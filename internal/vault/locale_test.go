package vault

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishCLIReplies(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	for kind, want := range map[string]string{
		"no-ui":        "The interface is not connected: nobody is looking at this machine now (the page is closed)",
		"busy":         "The previous sign-in request is still waiting for the person's answer",
		"rate-limited": "Too often: at most 6 sign-in requests in 10 minutes",
	} {
		status, body := CLIReply(Outcome{Kind: kind})
		if status == 200 || body.String("error") != want {
			t.Errorf("%s: %d %q", kind, status, body.String("error"))
		}
	}
	if status, body := CLIReply(Outcome{Kind: "timeout"}); status != 200 || body.String("message") != "The person did not answer the sign-in request: ask them to confirm it in the window and try again" {
		t.Errorf("timeout: %d %v", status, body)
	}
}
