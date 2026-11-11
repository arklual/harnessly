package apps

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The loader of a site that exists already: its files are served beside the SDK and under the folder of the
// SDK's major version, which a site's loader line names.
func TestViewServerServesTheLoaderByMajorVersion(t *testing.T) {
	reg, _ := installedPaneApp(t, nil)
	s, tok := tokened(t, reg)
	h := s.Handler()
	for path, want := range map[string]int{
		LoaderPath:                    200,
		LoaderInlinePath:              200,
		SDKClassicPath:                200,
		"/__hl/v1/loader.js":          200,
		"/__hl/v1/bridge-shim.js":     200,
		"/__hl/v1/app-sdk.classic.js": 200,
		"/__hl/v1/app-sdk.js":         200,
		"/__hl/v1/app-sdk.mjs":        200,
		"/__hl/v1/nope.js":            404,
		"/__hl/v2/loader.js":          404,
		tok + "/__hl/loader.js":       200,
	} {
		if got := get(t, h, path).Code; got != want {
			t.Errorf("%s: %d, want %d", path, got, want)
		}
	}
	if SDKVersionedPrefix != "/__hl/v1/" {
		t.Errorf("the versioned folder is %s: a new major keeps /__hl/v1/ serving what it served", SDKVersionedPrefix)
	}
	rec := get(t, h, SDKVersionedPrefix+"loader.js")
	if !bytes.Equal(rec.Body.Bytes(), loaderJS) || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Errorf("the versioned loader is not loader.js: %s", rec.Header().Get("Content-Type"))
	}
	// A site on another origin loads the files: no credentials, any origin.
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("the loader must be loadable from the site's origin")
	}
}

// The classic SDK publishes the surface without an import, and has no module syntax left.
func TestClassicSDKPublishesStateActionScreen(t *testing.T) {
	src := string(classicSDK())
	if !strings.Contains(src, "state: state, action: action, screen: screen") {
		t.Error("window.harnessly has no state, action and screen")
	}
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "export ") {
			t.Errorf("module syntax in the classic script: %s", line)
		}
	}
}

// What `harnessly app sdk` writes for a site that hosts its own copy, and the line the guide tells to paste.
func TestSDKFilesCarryTheLoader(t *testing.T) {
	files := SDKFiles()
	for _, name := range []string{"loader.js", "loader-inline.js", "app-sdk.classic.js", "bridge-shim.js", "app-sdk.js", "app-sdk.d.ts"} {
		if len(files[name]) == 0 {
			t.Errorf("no %s", name)
		}
	}
	// The loader loads the files it names from its own folder.
	for _, name := range []string{"bridge-shim.js", "app-sdk.classic.js"} {
		if !bytes.Contains(loaderJS, []byte("'"+name+"'")) {
			t.Errorf("loader.js does not load %s", name)
		}
	}
	line := strings.TrimSpace(string(loaderInlineJS))
	if strings.Contains(line, "\n") {
		t.Error("the loader line is one line")
	}
	if !strings.Contains(line, "'https://HARNESSLY_VIEWS"+strings.TrimSuffix(SDKVersionedPrefix, "/")+"'") || !strings.Contains(line, "'/loader.js'") {
		t.Errorf("the loader line names another folder than %s", SDKVersionedPrefix)
	}
	// The line names the Harnessly origins the site trusts, and the loader answers no other parent.
	if !strings.HasSuffix(line, ",['https://HARNESSLY_HOST']);") || !bytes.Contains(loaderJS, []byte("hosts.indexOf(e.origin) === -1")) {
		t.Error("the loader does not check the origin of the page that frames the site")
	}
	for _, page := range []string{"build-apps/11-existing-site.md", "ru/build-apps/11-existing-site.md"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", filepath.FromSlash(page)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "<script>"+line+"</script>") {
			t.Errorf("docs/%s does not carry the line of sdk/loader-inline.js", page)
		}
	}
}
