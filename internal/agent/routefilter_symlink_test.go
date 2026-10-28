//go:build linux || darwin

package agent

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A link in a project folder that points at the policy, the agent's env folder or a file beside the policy is the same
// file to the runtime, and the filter refuses it whatever the link is called.
func TestProtectedWriteFollowsLinks(t *testing.T) {
	home := t.TempDir()
	etc := filepath.Join(home, "cfg")
	envDir := filepath.Join(home, ".harnessly", "agent-env")
	proj := filepath.Join(home, "proj")
	for _, d := range []string{etc, envDir, proj} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	policy := filepath.Join(etc, "policy.toml")
	if err := os.WriteFile(policy, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustLink := func(target, name string) {
		if err := os.Symlink(target, filepath.Join(proj, name)); err != nil {
			t.Fatal(err)
		}
	}
	mustLink(policy, "notes.txt")
	mustLink(envDir, "envlink")
	mustLink(filepath.Join(etc, "nothing-yet"), "dangling")
	mustLink(proj, "self")

	f := &routeFilter{roots: []string{home}}
	f.protected = func() []string { return []string{policy} }
	f.protectedTrees = func() []string { return []string{policy, filepath.Join(etc, "nothing-yet"), envDir} }

	for _, tc := range []struct {
		path string
		deny bool
	}{
		{"proj/notes.txt", true},
		{"proj/envlink/all.env", true},
		{"proj/envlink/sub/new.env", true},
		{"proj/dangling", true},
		{"proj/self/notes.txt", true},
		{filepath.Join(proj, "notes.txt"), true},
		{"proj/main.go", false},
		{"proj/self/main.go", false},
	} {
		for _, route := range []string{"/api/editor/file", "/api/upload-file", "/api/v2/uploads"} {
			r, _ := http.NewRequest("PUT", route+"?path="+tc.path, nil)
			got := f.protectedWrite(r, []string{route}) != ""
			if got != tc.deny {
				t.Errorf("%s %s: refused=%v, want %v", route, tc.path, got, tc.deny)
			}
		}
	}
}

func TestRealTargetLoopFailsClosed(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}
	if _, ok := realTarget(filepath.Join(a, "x")); ok {
		t.Error("a link loop was followed to an end")
	}
	f := &routeFilter{roots: []string{dir}}
	r, _ := http.NewRequest("PUT", "/api/editor/file?path=a/x", nil)
	if f.protectedWrite(r, []string{"/api/editor/file"}) == "" {
		t.Error("a write through a link loop was let through")
	}
	// A chain longer than the cap.
	prev := filepath.Join(dir, "target")
	if err := os.WriteFile(prev, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxLinkHops+5; i++ {
		l := filepath.Join(dir, "c"+strconv.Itoa(i))
		if err := os.Symlink(prev, l); err != nil {
			t.Fatal(err)
		}
		prev = l
	}
	if real, ok := realTarget(prev + "/nope"); ok && real == "" {
		t.Error("empty")
	}
}

func TestCandidateCapFailsClosed(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.Mkdir(filepath.Join(dir, "d"+strconv.Itoa(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := &routeFilter{roots: []string{dir}}
	if _, complete := f.candidates("x", nil); !complete {
		t.Error("a small folder is complete")
	}
	defer func(n int) { maxCandidates = n }(maxCandidates)
	maxCandidates = 3
	if _, complete := f.candidates("x", nil); complete {
		t.Error("a cut-off list was taken as complete")
	}
	r, _ := http.NewRequest("PUT", "/api/editor/file?path=x", nil)
	if f.protectedWrite(r, []string{"/api/editor/file"}) == "" {
		t.Error("a write whose places could not all be looked at was let through")
	}
	maxCandidates = 4000
	if _, complete := (&routeFilter{}).candidates("x", nil); complete {
		t.Error("a relative path with no root to take it from was taken as known")
	}
}

func TestLiesFoldsCase(t *testing.T) {
	trees := []string{"/home/a/.config/harnessly/policy.toml"}
	if lies("/home/a/.config/Harnessly/Policy.toml", trees, false) {
		t.Error("exact comparison folded")
	}
	if !lies("/home/a/.config/Harnessly/Policy.toml", trees, true) {
		t.Error("case-insensitive comparison missed a difference of case")
	}
}

func TestUploadBodyNamesAreJudged(t *testing.T) {
	home := t.TempDir()
	etc := filepath.Join(home, "cfg")
	proj := filepath.Join(home, "proj")
	for _, d := range []string{etc, proj} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	policy := filepath.Join(etc, "policy.toml")
	if err := os.WriteFile(policy, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(policy, filepath.Join(proj, "innocent.png")); err != nil {
		t.Fatal(err)
	}
	f := &routeFilter{roots: []string{home}}
	f.protected = func() []string { return []string{policy} }
	f.protectedTrees = func() []string { return []string{policy} }
	do := func(name string) (string, []byte) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		fw, _ := w.CreateFormFile("file", name)
		_, _ = fw.Write([]byte("DATA"))
		_ = w.Close()
		r, _ := http.NewRequest("POST", "/api/upload-image", bytes.NewReader(buf.Bytes()))
		r.Header.Set("Content-Type", w.FormDataContentType())
		reason := f.protectedWrite(r, []string{"/api/upload-image"})
		got := new(bytes.Buffer)
		_, _ = got.ReadFrom(r.Body)
		return reason, got.Bytes()
	}
	if reason, _ := do("proj/innocent.png"); reason == "" {
		t.Error("an upload named by a link to the policy was let through")
	}
	if reason, _ := do("policy.toml"); reason == "" {
		t.Error("an upload named policy.toml was let through")
	}
	reason, body := do("proj/photo.png")
	if reason != "" || !bytes.Contains(body, []byte("DATA")) {
		t.Errorf("a plain upload: %q, body kept: %v", reason, bytes.Contains(body, []byte("DATA")))
	}
	r, _ := http.NewRequest("POST", "/api/upload-image", bytes.NewReader([]byte("garbage")))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=zz")
	if f.protectedWrite(r, []string{"/api/upload-image"}) == "" {
		t.Error("a multipart body that does not parse was let through")
	}
}

func TestUploadContentTypesFailClosed(t *testing.T) {
	home := t.TempDir()
	policy := filepath.Join(home, "policy.toml")
	if err := os.WriteFile(policy, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &routeFilter{roots: []string{home}}
	f.protected = func() []string { return []string{policy} }
	f.protectedTrees = func() []string { return []string{policy} }
	do := func(ct, body string) string {
		r, _ := http.NewRequest("POST", "/api/upload-file", bytes.NewReader([]byte(body)))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		return f.protectedWrite(r, []string{"/api/upload-file"})
	}
	for _, tc := range []struct {
		name, ct, body string
		deny           bool
	}{
		{"no content type", "", `{"a":1}`, true},
		{"a raw type", "application/octet-stream", "xx", true},
		{"a type that does not parse", "multipart/form-data; boundary", "xx", true},
		{"multipart in capitals with two boundaries", "Multipart/Form-Data; boundary=a; boundary=b", "xx", true},
		{"multipart/mixed", "multipart/mixed; boundary=a", "--a--", true},
		{"a number as the path", "application/json", `{"path":5}`, false},
		{"the policy nested in an object", "application/json", `{"a":{"b":["x",{"c":"policy.toml"}]}}`, true},
		{"the policy by name", "application/json", `{"path":"policy.toml"}`, true},
		{"a folder and a name that join to the policy", "application/json", `{"dir":"` + home + `","other":"policy.toml"}`, true},
		{"a form naming the policy", "application/x-www-form-urlencoded", "path=policy.toml", true},
		{"a body that is not an object", "application/json", `[1]`, true},
		{"a plain JSON upload", "application/json", `{"name":"photo.png"}`, false},
	} {
		if got := do(tc.ct, tc.body) != ""; got != tc.deny {
			t.Errorf("%s: refused=%v, want %v", tc.name, got, tc.deny)
		}
	}
	// A text field of a multipart body is a name too.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("path", "policy.toml")
	_ = w.Close()
	if do(w.FormDataContentType(), buf.String()) == "" {
		t.Error("a multipart text field naming the policy was let through")
	}
}

func TestUploadMultipartAndQueryAreJudgedWhole(t *testing.T) {
	home := t.TempDir()
	policy := filepath.Join(home, "policy.toml")
	if err := os.WriteFile(policy, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &routeFilter{roots: []string{home}}
	f.protected = func() []string { return []string{policy} }
	f.protectedTrees = func() []string { return []string{policy} }
	multi := func(build func(w *multipart.Writer)) (string, string) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		build(w)
		_ = w.Close()
		return w.FormDataContentType(), buf.String()
	}
	do := func(query, ct, body string) string {
		r, _ := http.NewRequest("POST", "/api/upload-image"+query, bytes.NewReader([]byte(body)))
		r.Header.Set("Content-Type", ct)
		return f.protectedWrite(r, []string{"/api/upload-image"})
	}
	ct, body := multi(func(w *multipart.Writer) { _ = w.WriteField("note", strings.Repeat("a", 5000)) })
	if do("", ct, body) == "" {
		t.Error("a long text part was skipped")
	}
	ct, body = multi(func(w *multipart.Writer) {
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{`form-data; name="x"`}
		h["Content-Transfer-Encoding"] = []string{"base64"}
		pw, _ := w.CreatePart(h)
		_, _ = pw.Write([]byte("cG9saWN5LnRvbWw="))
	})
	if do("", ct, body) == "" {
		t.Error("an encoded part was let through")
	}
	ct, body = multi(func(w *multipart.Writer) { _ = w.WriteField("path", "policy.toml") })
	if do("", ct, body) == "" {
		t.Error("the policy in a text part")
	}
	// A folder in the query and a name in the body join.
	ct, body = multi(func(w *multipart.Writer) { _ = w.WriteField("name", "policy.toml") })
	if do("?dir="+home, ct, body) == "" {
		t.Error("a query folder and a body name that join to the policy")
	}
	ct, body = multi(func(w *multipart.Writer) { _ = w.WriteField("a", "1") })
	if got := do("?a=1&b=2&c=3&d=4&e=5&f=6&g=7&h=8&i=9", ct, body); got == "" {
		t.Error("more pieces than the cap were let through")
	}
	if got := do("", ct, body); got != "" {
		t.Errorf("a small upload: %q", got)
	}
}
