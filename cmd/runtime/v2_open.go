package main

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/sources"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Open is where a chip or a link of a conversation opens (GET /api/v2/conversations/{id}/open): the rules of the
// agent's open command (internal/panel), so the person's click and the agent's command land in the same tab.
//
// A merge request is named `!12`, or with its project in front as the text names it (`harnessly !80`). Which project
// has it is looked for in v2_mrresolve.go's order: the project the text names, the merge request links the agent and
// the person wrote, the checkout of the chat's folder, and, when the folder is no checkout but holds some (a folder of
// many repositories), those. A candidate is a merge request only once its GitLab says so, so what is answered opens.
type v2Open struct {
	record  func(conv string) (wire.Conversation, bool)
	targets panel.Targets
	// mrLinks are the merge request links the agent and the person wrote in a transcript, oldest first; nil reads none.
	mrLinks func(transcript string) []sources.MRMention
	// remotes are the remote URLs of the checkout at a folder; nil reads them with git.
	remotes func(ctx context.Context, dir string) []string
	// repos are the names of the checkouts directly under a folder; nil lists the directories that hold a .git.
	repos func(dir string) []string
	// mr asks a GitLab of the deployment for the merge request numbered iid of a project; nil: none exists.
	mr mrLookup
	// projects are the paths of the projects of a GitLab of the deployment that are called name; nil: none are.
	projects func(ctx context.Context, host, name string) []string
	// state keeps what was seen of the folders; nil: nothing is kept.
	state *mrState
}

// mrRef is `!12` or `<project> !12`: a word, or a path of words with slashes, then the number, as the text writes it.
var mrRef = regexp.MustCompile(`^(?:([\w][\w.-]*(?:/[\w.-]+)*) )?!(\d{1,6})$`)

func (o v2Open) Open(ctx context.Context, conv, ref string) (wire.OpenTarget, error) {
	rec, ok := o.record(conv)
	if !ok {
		return wire.OpenTarget{}, convo.ErrNotFound
	}
	target := o.open(ctx, rec, ref)
	if target == nil {
		return wire.OpenTarget{}, wire.ErrNothingToOpen
	}
	return *target, nil
}

// openAllParallel is how many refs of one request are looked into at once.
const openAllParallel = 4

// OpenAll is Open for many refs: nil for what opens nothing.
func (o v2Open) OpenAll(ctx context.Context, conv string, refs []string) ([]*wire.OpenTarget, error) {
	rec, ok := o.record(conv)
	if !ok {
		return nil, convo.ErrNotFound
	}
	out := make([]*wire.OpenTarget, len(refs))
	var wg sync.WaitGroup
	slots := make(chan struct{}, openAllParallel)
	for i, ref := range refs {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			out[i] = o.open(ctx, rec, ref)
		}()
	}
	wg.Wait()
	return out, nil
}

func (o v2Open) open(ctx context.Context, rec wire.Conversation, ref string) *wire.OpenTarget {
	if m := mrRef.FindStringSubmatch(ref); m != nil {
		return o.openMR(ctx, rec, m[1], m[2])
	}
	tab, target := o.targets.Classify(ref)
	if tab == "" {
		return nil
	}
	return &wire.OpenTarget{Tab: wire.OpenTab(tab), Target: target}
}

func (o v2Open) openMR(ctx context.Context, rec wire.Conversation, name, iid string) *wire.OpenTarget {
	found := o.resolveMR(ctx, rec, name, iid)
	var choices []wire.OpenChoice
	for _, f := range found {
		tab, target := o.targets.Classify(f.url)
		if tab != string(wire.OpenGitLab) || len(choices) == wire.OpenChoicesMax {
			continue
		}
		var detail *string
		if f.title != "" {
			detail = &f.title
		}
		choices = append(choices, wire.OpenChoice{Tab: wire.OpenTab(tab), Target: target, Label: f.project.path, Detail: detail})
	}
	switch len(choices) {
	case 0:
		return nil
	case 1:
		return &wire.OpenTarget{Tab: choices[0].Tab, Target: choices[0].Target}
	}
	return &wire.OpenTarget{Tab: choices[0].Tab, Target: choices[0].Target, Choices: choices}
}

// gitlabProject is a project of a GitLab of the deployment; dir is the folder it is checked out in, "" for none.
type gitlabProject struct {
	host, path, dir string
}

func (p gitlabProject) key() string { return p.host + "/" + p.path }

func (p gitlabProject) mrURL(iid int) string {
	return "https://" + p.host + "/" + p.path + "/-/merge_requests/" + strconv.Itoa(iid)
}

// mrInfo is a merge request that is there: where it is now, its title and when it last changed ("" when unknown).
type mrInfo struct {
	url, title, updatedAt string
}

// mrFound is a merge request that is there, and the project it is in.
type mrFound struct {
	project gitlabProject
	mrInfo
}

// mrLookup asks a GitLab for a merge request of a project.
type mrLookup func(ctx context.Context, host, project string, iid int) (mrInfo, bool)

var scpRemote = regexp.MustCompile(`^[\w.-]+@([\w.-]+):(.+)$`)

// remoteProject is the host and the project path of a remote URL: https://host/g/p.git, ssh://git@host[:port]/g/p.git
// or git@host:g/p.git.
func remoteProject(raw string) (host, path string) {
	raw = strings.TrimSpace(raw)
	if m := scpRemote.FindStringSubmatch(raw); m != nil {
		host, path = m[1], m[2]
	} else if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host, path = u.Hostname(), u.Path
	} else {
		return "", ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if path == "" || strings.Contains(path, "..") {
		return "", ""
	}
	return strings.ToLower(host), path
}

func (o v2Open) onGitLab(host string) bool {
	for _, h := range o.targets.GitLabHosts {
		if host != "" && host == h {
			return true
		}
	}
	return false
}

// projectOf is the first remote of the checkout at dir that is on a GitLab of the deployment.
func (o v2Open) projectOf(ctx context.Context, dir string) (gitlabProject, bool) {
	remotes := o.remotes
	if remotes == nil {
		remotes = gitRemotes
	}
	for _, raw := range remotes(ctx, dir) {
		if host, p := remoteProject(raw); o.onGitLab(host) {
			return gitlabProject{host: host, path: p, dir: path.Base(dir)}, true
		}
	}
	return gitlabProject{}, false
}

func gitRemotes(ctx context.Context, dir string) []string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get-regexp", `^remote\..*\.url$`).Output()
	if err != nil {
		return nil
	}
	var urls []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if _, u, ok := strings.Cut(line, " "); ok {
			urls = append(urls, u)
		}
	}
	return urls
}

// gitDirs are the names of the directories directly under dir that are checkouts (hold a .git), hidden ones left out.
func gitDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if _, err := os.Stat(path.Join(dir, e.Name(), ".git")); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// nestedMax is how many checkouts under a folder are looked at.
const nestedMax = 64

// layoutTTL is how long the checkouts of a folder are remembered.
const layoutTTL = time.Minute

// layout is what a chat's folder holds: its own GitLab project when it is a checkout, else the GitLab projects of the
// checkouts one level below it.
type layout struct {
	own    *gitlabProject
	nested []gitlabProject
}

type layoutEntry struct {
	at time.Time
	l  layout
}

// mrState is what the opens of one runtime remember.
type mrState struct {
	mu      sync.Mutex
	layouts map[string]layoutEntry
}

// layoutOf reads the folder (git, a process per checkout), once a minute; the lock is held through the reading, so
// the refs of one request that ask at once wait for the first answer instead of reading the folder each.
func (o v2Open) layoutOf(ctx context.Context, dir string) layout {
	if dir == "" {
		return layout{}
	}
	if o.state != nil {
		o.state.mu.Lock()
		defer o.state.mu.Unlock()
		if e, ok := o.state.layouts[dir]; ok && time.Since(e.at) < layoutTTL {
			return e.l
		}
	}
	var l layout
	if own, ok := o.projectOf(ctx, dir); ok {
		l.own = &own
	} else {
		repos := o.repos
		if repos == nil {
			repos = gitDirs
		}
		seen := map[string]bool{}
		for _, name := range repos(dir) {
			if len(l.nested) == nestedMax {
				break
			}
			if p, ok := o.projectOf(ctx, path.Join(dir, name)); ok && !seen[p.key()] {
				seen[p.key()] = true
				l.nested = append(l.nested, p)
			}
		}
	}
	if o.state != nil && ctx.Err() == nil {
		if o.state.layouts == nil {
			o.state.layouts = map[string]layoutEntry{}
		}
		o.state.layouts[dir] = layoutEntry{at: time.Now(), l: l}
	}
	return l
}

// mrLimit is how long one ref may take to be looked into: less than the page budget of a request (10 s), so that what
// was asked and not yet answered ends as a no instead of a refusal; the answers still coming in are kept for the next ask.
const mrLimit = 8 * time.Second

// resolveMR is the merge requests numbered iid the text may mean, the best first; name is the project written in front
// of the number ("" for none).
func (o v2Open) resolveMR(ctx context.Context, rec wire.Conversation, name, iid string) []mrFound {
	n, err := strconv.Atoi(iid)
	if err != nil || n < 1 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, mrLimit)
	defer cancel()
	linked := o.linked(rec.TranscriptPath, iid)
	lay := o.layoutOf(ctx, rec.Cwd)

	if name != "" {
		pool := o.pool(linked, lay)
		var named []gitlabProject
		for _, p := range pool {
			if nameMatches(name, p) {
				named = append(named, p)
			}
		}
		if len(named) > 0 {
			return o.verify(ctx, named, n)
		}
		// A name that no checkout of the chat has may be a project of the deployment's own GitLab all the same (a
		// path, or the name its search finds); a word of the sentence is none, and the search goes on without it.
		if found := o.verify(ctx, o.known(ctx, name), n); len(found) > 0 {
			return found
		}
	}
	if len(linked) > 0 {
		out := make([]mrFound, len(linked))
		for i, p := range linked {
			out[i] = mrFound{project: p, mrInfo: mrInfo{url: p.mrURL(n)}}
		}
		return out
	}
	if lay.own != nil {
		return o.verify(ctx, []gitlabProject{*lay.own}, n)
	}
	return recentFirst(o.verify(ctx, lay.nested, n))
}

// known are the projects of the first GitLab of the deployment that a name written in the text may mean: the path
// itself, or the projects called so.
func (o v2Open) known(ctx context.Context, name string) []gitlabProject {
	if len(o.targets.GitLabHosts) == 0 {
		return nil
	}
	host := o.targets.GitLabHosts[0]
	if strings.Contains(name, "/") {
		return []gitlabProject{{host: host, path: strings.ToLower(name)}}
	}
	if o.projects == nil {
		return nil
	}
	var out []gitlabProject
	for _, p := range o.projects(ctx, host, name) {
		out = append(out, gitlabProject{host: host, path: p})
	}
	return out
}

// linked are the projects whose merge request iid the chat's texts link, the latest mention first.
func (o v2Open) linked(transcript, iid string) []gitlabProject {
	if o.mrLinks == nil || transcript == "" {
		return nil
	}
	mentions := o.mrLinks(transcript)
	var out []gitlabProject
	seen := map[string]bool{}
	for i := len(mentions) - 1; i >= 0; i-- {
		m := mentions[i]
		p := gitlabProject{host: m.Host, path: m.Project}
		if m.IID == iid && o.onGitLab(m.Host) && !seen[p.key()] {
			seen[p.key()] = true
			out = append(out, p)
		}
	}
	return out
}

// pool is the projects the chat knows by name: the ones its texts link (any merge request, not only this one), its
// folder's checkout and the checkouts under it.
func (o v2Open) pool(linkedHere []gitlabProject, lay layout) []gitlabProject {
	seen := map[string]bool{}
	var out []gitlabProject
	add := func(p gitlabProject) {
		if !seen[p.key()] {
			seen[p.key()] = true
			out = append(out, p)
		}
	}
	for _, p := range linkedHere {
		add(p)
	}
	if lay.own != nil {
		add(*lay.own)
	}
	for _, p := range lay.nested {
		add(p)
	}
	return out
}

// nameMatches: the written name is the project's path, its last segments or the name of the folder it is checked out in.
func nameMatches(name string, p gitlabProject) bool {
	want := strings.ToLower(strings.Trim(name, "/"))
	have := strings.ToLower(p.path)
	return want != "" && (have == want || strings.HasSuffix(have, "/"+want) || strings.ToLower(p.dir) == want)
}

// verify keeps the projects whose GitLab has the merge request, in the order given.
func (o v2Open) verify(ctx context.Context, projects []gitlabProject, iid int) []mrFound {
	if o.mr == nil {
		return nil
	}
	found := make([]*mrFound, len(projects))
	var wg sync.WaitGroup
	for i, p := range projects {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if info, ok := o.mr(ctx, p.host, p.path, iid); ok {
				found[i] = &mrFound{project: p, mrInfo: info}
			}
		}()
	}
	wg.Wait()
	var out []mrFound
	for _, f := range found {
		if f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// recentFirst puts the merge requests changed last first (the order stays for the ones that say nothing); of many
// checkouts that have a !80, the one worked on is the likeliest.
func recentFirst(found []mrFound) []mrFound {
	sort.SliceStable(found, func(i, j int) bool { return found[i].updatedAt > found[j].updatedAt })
	return found
}

// transcriptMRs reads the merge request links of the texts of a transcript: the agent's answers and the person's
// messages, not what the tools printed, and only links to the deployment's GitLabs.
func transcriptMRs(home string) func(string) []sources.MRMention {
	rt := &sources.Route{Links: sourceLinks(), Home: home}
	return func(path string) []sources.MRMention {
		res, found, failed := rt.Read(path, "")
		if !found || failed {
			return nil
		}
		return res.MRs
	}
}
