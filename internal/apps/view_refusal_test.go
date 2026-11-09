package apps

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestRefusedViewFileIsAPageWithTheReasonAndALogLine(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	s, tok := tokened(t, reg)
	var logged bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	rec := get(t, s.Handler(), tok+"/acme.demo/ui/missing.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type %q", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	for _, want := range []string{"Не загрузился файл ui/missing.js", `data-status="404"`, `data-code="no-file"`, "<script>"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'sha256-") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("csp %q", csp)
	}
	line := logged.String()
	for _, want := range []string{"apps view refused", "app=acme.demo", "path=ui/missing.js", "status=404", "code=no-file"} {
		if !strings.Contains(line, want) {
			t.Errorf("log lacks %q: %s", want, line)
		}
	}
	// The session's token is a secret: it is in neither the page nor the log.
	secret := strings.TrimPrefix(tok, "/")
	if strings.Contains(string(body), secret) || strings.Contains(line, secret) {
		t.Error("the token leaked")
	}
}

func TestRefusedWithoutSessionSaysSo(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	s, _ := tokened(t, reg)
	rec := get(t, s.Handler(), "/nosuchtoken/acme.demo/ui/index.html")
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusNotFound || !strings.Contains(string(body), "Сессия вида закончилась") {
		t.Errorf("%d %s", rec.Code, body)
	}
}
