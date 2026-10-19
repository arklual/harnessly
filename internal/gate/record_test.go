package gate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordsRequestsWithoutCredentials(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Set-Cookie", "session-token=new")
		w.WriteHeader(201)
		fmt.Fprintf(w, "got %s", body)
	}))
	defer old.Close()
	file := filepath.Join(t.TempDir(), "rec.jsonl")
	f, err := New(Config{Listen: "127.0.0.1:0", Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: t.TempDir(),
		CountEvery: time.Hour, RecordFile: file, TokenHeaders: []string{"X-Cli-Secret"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/agents/api/x?a=1", strings.NewReader("hello"))
	req.Header.Set("Cookie", "session-token=secret")
	req.Header.Set("X-Cli-Secret", "secret")
	req.Header.Set("X-App-Token", "secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(res.Body); string(b) != "got hello" {
		t.Fatalf("the page got %q", b)
	}
	var raw []byte
	for deadline := time.Now().Add(5 * time.Second); len(raw) == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		raw, _ = os.ReadFile(file) // the line is written once the answer has gone out
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "session-token=new") {
		t.Fatalf("a credential was recorded: %s", raw)
	}
	var e recordEntry
	line, _ := bufio.NewReader(strings.NewReader(string(raw))).ReadString('\n')
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatal(err)
	}
	if e.Method != "POST" || e.Path != "/agents/api/x" || e.Query != "a=1" || e.Status != 201 ||
		*e.Body.Text != "hello" || *e.Resp.Text != "got hello" || e.Headers["Cookie"][0] != "<present>" || e.ByGo {
		t.Fatalf("%+v", e)
	}
}
