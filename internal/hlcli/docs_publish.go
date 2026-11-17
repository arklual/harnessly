package hlcli

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// `harnessly docs publish`: a document for the person goes through the machine agent's loopback /docs/publish to the
// hub's doc.publish (owner, space, binding to the machine), so it shows in the Documents app. `artifacts publish`
// writes to the catalog directly and leaves no such record: it stays the fallback for a machine without a hub.

var docTitleRe = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

const docsNoHub = "there is no hub on this machine, so the document cannot go into the Documents app. " +
	"Fallback (the document will not be listed there): artifacts publish --file FILE --title \"…\""

func (c *cli) docsPublish(a docsArgs) error {
	if len(a.pos) != 1 {
		return usageErr("docs publish takes one file: harnessly docs publish report.md --title \"…\" [--slug S]")
	}
	file := c.absPath(a.pos[0])
	format := ""
	switch strings.ToLower(filepath.Ext(file)) {
	case ".md", ".markdown":
		format = "md"
	case ".html", ".htm":
		format = "html"
	default:
		return usageErr("a document is a .md or .html file")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return fail("the document: %v", err)
	}
	content := string(raw)
	title := strings.TrimSpace(a.title)
	if title == "" {
		if m := docTitleRe.FindStringSubmatch(content); m != nil && format == "md" {
			title = m[1]
		} else {
			title = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		}
	}
	vis := "private"
	if a.public {
		vis = "public"
	}
	body := map[string]any{
		"title": title, "format": format, "content": content, "visibility": vis,
		"source": map[string]string{"path": file, "cwd": filepath.Dir(file)},
	}
	if a.slug != "" {
		body["slug"] = a.slug
	}
	if a.note != "" {
		body["note"] = a.note
	}
	status, resp, err := agent.CallLocal(c.env.Client, c.dataDir(), c.env.Getenv("RUNTIME_AGENT_TOKEN_HEADER"), "/docs/publish", body, 70*time.Second)
	if errors.Is(err, agent.ErrNoAgent) {
		return fail("%s", docsNoHub)
	}
	if err != nil {
		return fail("the document did not go to the hub: %v", err)
	}
	if status == http.StatusUnprocessableEntity {
		var s struct {
			Kind string `json:"kind"`
			Line int    `json:"line"`
		}
		_ = json.Unmarshal(resp, &s)
		return fail("the document holds a secret (%s) on line %d; remove it and publish again", s.Kind, s.Line)
	}
	if status == http.StatusServiceUnavailable {
		return fail("the hub does not take documents now (503): %s. If it stays so, %s", errText(resp), docsNoHub)
	}
	if status != http.StatusOK {
		return apiError(status, resp)
	}
	// The agent answers {status, body}: the hub's own status and body.
	var ans struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(resp, &ans) != nil || ans.Status == 0 {
		return fail("the hub answered unexpectedly")
	}
	if ans.Status == http.StatusServiceUnavailable {
		return fail("the hub does not keep documents (503): %s. %s", errText(ans.Body), docsNoHub)
	}
	if ans.Status != http.StatusOK {
		return apiError(ans.Status, ans.Body)
	}
	var out struct {
		Slug      string `json:"slug"`
		Version   int    `json:"version"`
		Created   bool   `json:"created"`
		PublicURL string `json:"publicUrl"`
	}
	if json.Unmarshal(ans.Body, &out) != nil || out.Slug == "" {
		return fail("the hub answered without a document")
	}
	if c.json {
		c.printRaw(ans.Body)
		return nil
	}
	if base := c.docsBase(); base != "" {
		c.out("%s/a/%s\n", base, out.Slug)
	}
	if out.PublicURL != "" {
		c.out("%s\n", out.PublicURL)
	}
	verb := "updated"
	if out.Created {
		verb = "published"
	}
	c.out("%s %s, version %d (in the Documents app)\n", verb, out.Slug, out.Version)
	return nil
}

func errText(raw []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	if e.Error == "" {
		return strings.TrimSpace(string(raw))
	}
	return e.Error
}

// docsBase is where the documents are read: the artifacts service the agent was given (ARTIFACTS_URL), else the
// documents host the runtime names (RUNTIME_DOCS_HOST, passed to every pane). "" for none: no address is printed and the
// chat has no card for the document.
func (c *cli) docsBase() string {
	if base := strings.TrimRight(strings.TrimSpace(c.env.Getenv("ARTIFACTS_URL")), "/"); base != "" {
		return base
	}
	host := strings.TrimSpace(c.env.Getenv("RUNTIME_DOCS_HOST"))
	if host == "" || strings.ContainsAny(host, "/ ") {
		return ""
	}
	return "https://" + host
}
