package design

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sitesclient"
)

// «Опубликовать»: a copy of the project's pages, as they are now, goes to the hub as a version of a site. The hub keeps
// the files, so the site opens with this machine off. The pages are chosen the way a guest link's are, and a page
// that is not chosen never leaves the machine.

const (
	publishMaxFiles = 5000
	publishMaxBytes = 100 << 20
	publishTimeout  = 3 * time.Minute
)

// ErrPublishEmpty: nothing of the project is left to publish.
var ErrPublishEmpty = errors.New("nothing to publish")

// publishSkipped are the folders and files that are no part of a site: the project's history, tools, hidden files.
func publishSkipped(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") || part == "node_modules" {
			return true
		}
	}
	return false
}

// PublishSnapshot reads the files of the project a site is made of. pages are the board paths chosen (nil is all of
// them); the result carries an index.html that opens the page, or lists the pages, when the project has none of its own.
func (l *Library) PublishSnapshot(project string, pages []string) (title string, files []sitesclient.DeployFile, err error) {
	art := l.ReadArtifact(project)
	dir := l.ProjectDirOf(project)
	if art == nil || dir == "" {
		return "", nil, ErrPublishEmpty
	}
	scope := publicScope{}
	if pages != nil {
		scope.limited, scope.pages = true, map[string]bool{}
		for _, p := range pages {
			scope.pages[p] = true
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", nil, err
	}
	defer root.Close()

	chosen := map[string]bool{} // files of the boards that are published
	var shown []Board
	for _, b := range art.Boards {
		if scope.allows(b) {
			chosen[boardFile(b.Path)] = true
			shown = append(shown, b)
		}
	}
	engine := l.EngineFor(project)
	var total int64
	walkErr := fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if publishSkipped(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || ContentType(extname(rel)) == "" {
			return nil
		}
		if scope.hides(art.Boards, rel) || (scope.limited && isMarkup(rel) && !chosen[path.Clean(rel)]) {
			return nil
		}
		var data []byte
		if rel == EngineFile && engine != "" {
			data, err = os.ReadFile(engine)
		} else {
			data, err = root.ReadFile(rel)
		}
		if err != nil {
			return err
		}
		if len(data) > PublicFileLimit {
			return nil
		}
		total += int64(len(data))
		if len(files) >= publishMaxFiles || total > publishMaxBytes {
			return fmt.Errorf("the project is too big to publish (up to %d files, %d MB)", publishMaxFiles, publishMaxBytes>>20)
		}
		files = append(files, sitesclient.DeployFile{Path: rel, Data: data})
		return nil
	})
	if walkErr != nil {
		return "", nil, walkErr
	}
	if len(shown) == 0 {
		return "", nil, ErrPublishEmpty
	}
	has := false
	for _, f := range files {
		has = has || f.Path == "index.html"
	}
	if !has {
		files = append(files, sitesclient.DeployFile{Path: "index.html", Data: []byte(publishIndex(art.Title, shown))})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return art.Title, files, nil
}

// publishIndex is the first page of a site made of boards: it goes straight to the only page, or lists them.
func publishIndex(title string, boards []Board) string {
	if title == "" {
		title = locale.Pick("Design", "Макет")
	}
	seen := map[string]bool{}
	var links []string
	var first string
	for _, b := range boards {
		file := boardFile(b.Path)
		if seen[file] {
			continue
		}
		seen[file] = true
		if first == "" {
			first = file
		}
		name := b.Title
		if name == "" {
			name = file
		}
		links = append(links, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, html.EscapeString(file), html.EscapeString(name)))
	}
	head := `<!doctype html><html lang="` + locale.Pick("en", "ru") + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(title) + `</title>`
	if len(links) == 1 {
		return head + `<meta http-equiv="refresh" content="0;url=` + html.EscapeString(first) + `"></head><body><a href="` + html.EscapeString(first) + `">` + html.EscapeString(title) + `</a></body></html>`
	}
	return head + `<style>:root{color-scheme:light dark}body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem}h1{font-size:1.75rem}ul{padding:0;list-style:none;display:grid;gap:.5rem}a{display:block;padding:.75rem 1rem;border:1px solid #8884;border-radius:.75rem;color:inherit;text-decoration:none}a:hover{background:#8881}</style></head><body><h1>` +
		html.EscapeString(title) + `</h1><ul>` + strings.Join(links, "") + `</ul></body></html>`
}

// PublishOrigin is what a site made from the project is marked with, so that only the project can publish onto it.
func PublishOrigin(project string) string { return "sinopia:" + project }

// publishRequest is the body of POST <platform>/design/publish.
type publishRequest struct {
	project, name, access string
	pages                 []string
}

func readPublish(body any) (publishRequest, bool) {
	o := Fields(body)
	var req publishRequest
	var ok1, ok2 bool
	req.project, ok1 = strField(o, "project")
	req.name, ok2 = strField(o, "name")
	if !ok1 || !ok2 {
		return req, false
	}
	req.access, _ = strField(o, "access")
	if req.access != "team" && req.access != "link" {
		req.access = "team"
	}
	if Has(o, "pages") && Field(o, "pages") != nil {
		list, ok := Field(o, "pages").([]any)
		if !ok {
			return req, false
		}
		req.pages = []string{}
		for _, item := range list {
			p, ok := item.(string)
			if !ok {
				return req, false
			}
			req.pages = append(req.pages, p)
		}
	}
	return req, true
}

// Publish is POST <platform>/design/publish {project, name, access, pages?}: the project's pages go to the machine's hub
// as the next version of the site `name` and the answer is the site's address.
func (a *API) Publish(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	req, ok := readPublish(body)
	if !ok {
		return JSON(r, 400, errorBody(locale.Pick("Invalid request", "Неверный запрос")), h)
	}
	if no := a.gate(r, req.project, RoleManage); no != nil {
		return *no
	}
	title, files, err := a.L.PublishSnapshot(req.project, req.pages)
	switch {
	case errors.Is(err, ErrPublishEmpty):
		return JSON(r, 422, errorBody(locale.Pick("There are no pages to publish", "Нет страниц для публикации")), h)
	case err != nil:
		return JSON(r, 422, errorBody(err.Error()), h)
	}
	ctx, cancel := context.WithTimeout(r.Context(), publishTimeout)
	defer cancel()
	// The session is the machine's, asked for the person at this runtime; the hub checks the person is in the space.
	who, _ := a.L.Identity()
	sess, err := agent.FetchSitesSession(ctx, a.L.cfg.DataDir, a.L.cfg.AgentTokenHeader, who.PersonID)
	if err != nil {
		return JSON(r, 409, struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}{locale.Pick("Publishing needs this machine to be connected to a hub that publishes sites", "Публикация работает, когда машина подключена к хабу с сайтами"), "no_hub"}, h)
	}
	c := &sitesclient.Client{Base: sess.URL, Token: sess.Token}
	// The site is bound to this project: a name that is somebody else's, or another project's, is not overwritten.
	if err := c.Claim(ctx, sess.Space, req.name, req.access, PublishOrigin(req.project)); err != nil {
		return publishFailed(r, h, err)
	}
	if err := c.SetAccess(ctx, sess.Space, req.name, req.access); err != nil {
		return publishFailed(r, h, err)
	}
	done, err := c.Deploy(ctx, sess.Space, req.name, "sinopia", files)
	if err != nil {
		return publishFailed(r, h, err)
	}
	return JSON(r, 200, struct {
		URL     string `json:"url"`
		Version int    `json:"version"`
		Name    string `json:"name"`
		Title   string `json:"title"`
		Files   int    `json:"files"`
	}{done.URL, done.Version, req.name, title, len(files)}, h)
}

// publishFailed passes on what the hub said when it refused (a name that is taken, a secret in a page, a limit).
func publishFailed(r *http.Request, h http.Header, err error) Answer {
	if errors.Is(err, sitesclient.ErrNameTaken) {
		return JSON(r, 409, struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}{locale.Pick("This name is taken: choose another", "Имя занято: выберите другое"), "name_taken"}, h)
	}
	var ce *sitesclient.ClientError
	if errors.As(err, &ce) && ce.Message != "" {
		return JSON(r, 422, errorBody(ce.Message), h)
	}
	return JSON(r, 502, errorBody(locale.Pick("The hub did not take the site", "Хаб не принял сайт")), h)
}
