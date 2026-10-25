package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2Designly is the machine's design library over v2 (internal/design): a project is made from the library's template
// and a v2 conversation is opened in its folder, instead of the Legacy tab. What the library does not keep itself, the
// kind of a project and which ones the person pinned, is in a file of the data dir.
type v2Designly struct {
	v    *v2Live
	api  *design.API
	file string
	// links are the designs each conversation's transcript worked on, whatever its folder.
	links *design.ChatLinks

	mu sync.Mutex
}

var _ wire.DesignlySource = (*v2Designly)(nil)

type designlyState struct {
	Kinds  map[string]wire.DesignKind `json:"kinds"`
	Pinned map[string]bool            `json:"pinned"`
	// AttachedDesigns are the designs a conversation outside their folders was pointed at from its composer, by the
	// conversation's id: the library lists the conversation among each design's chats.
	AttachedDesigns map[string][]string `json:"attachedDesigns,omitempty"`
}

// attachedMax is how many designs one conversation keeps linked; the oldest goes first.
const attachedMax = 20

// attachDesign links design to conv, the newest last.
func attachDesign(st *designlyState, conv, design string) {
	if st.AttachedDesigns == nil {
		st.AttachedDesigns = map[string][]string{}
	}
	have := slices.DeleteFunc(slices.Clone(st.AttachedDesigns[conv]), func(id string) bool { return id == design })
	have = append(have, design)
	if len(have) > attachedMax {
		have = have[len(have)-attachedMax:]
	}
	st.AttachedDesigns[conv] = have
}

// attachedTo says whether conv was pointed at design.
func attachedTo(st designlyState, conv, design string) bool {
	return slices.Contains(st.AttachedDesigns[conv], design)
}

func (d *v2Designly) load() designlyState {
	st := designlyState{Kinds: map[string]wire.DesignKind{}, Pinned: map[string]bool{}}
	if raw, err := os.ReadFile(d.file); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	if st.Kinds == nil {
		st.Kinds = map[string]wire.DesignKind{}
	}
	if st.Pinned == nil {
		st.Pinned = map[string]bool{}
	}
	return st
}

// change reads the file, lets f change it and writes it back whole.
func (d *v2Designly) change(f func(*designlyState)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.load()
	f(&st)
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.file), 0o700); err != nil {
		return err
	}
	tmp := d.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.file)
}

func (d *v2Designly) state() designlyState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.load()
}

func kindOf(st designlyState, id string) wire.DesignKind {
	if k, ok := st.Kinds[id]; ok && slicesContainsKind(k) {
		return k
	}
	return wire.DesignSite
}

func slicesContainsKind(k wire.DesignKind) bool {
	for _, x := range wire.DesignKinds {
		if x == k {
			return true
		}
	}
	return false
}

func designlyFail(code wire.RefuseCode, msg, project string) *wire.DesignlyError {
	return &wire.DesignlyError{Code: code, Message: msg, ProjectID: project}
}

// createFailure words a refusal of the library for the page: a validation is a bad request, a taken id a conflict.
func createFailure(err error) error {
	var ce *design.CreateError
	switch {
	case errors.As(err, &ce) && ce.Conflict:
		return designlyFail(wire.RefuseConflict, ce.Msg, "")
	case errors.As(err, &ce):
		return designlyFail(wire.RefuseBadRequest, ce.Msg, "")
	}
	return designlyFail(wire.RefuseInternal, locale.Pick("Could not create the project", "Не удалось создать проект"), "")
}

func (d *v2Designly) Create(ctx context.Context, c wire.DesignlyCreate) (wire.DesignlyCreated, error) {
	lib := d.api.L
	brief, err := design.CleanBrief(c.Brief)
	if err != nil {
		return wire.DesignlyCreated{}, createFailure(err)
	}
	systemLine := ""
	if c.DesignSystem != nil {
		line, ok := d.systemLine(*c.DesignSystem)
		if !ok {
			return wire.DesignlyCreated{}, designlyFail(wire.RefuseBadRequest, locale.Pick("Unknown design system", "Неизвестная дизайн-система"), "")
		}
		systemLine = line
	}
	project, err := lib.CreateProject(c.Title)
	if err != nil {
		return wire.DesignlyCreated{}, createFailure(err)
	}
	notStarted := func(why string) error {
		return designlyFail(wire.RefuseInternal, locale.Pick("Project \""+project.Title+"\" was created, but the conversation did not start: ",
			"Проект «"+project.Title+"» создан, но диалог не запустился: ")+why, project.ID)
	}
	// The owner file (personal, v2) is a note for the library's other runtimes and the gateway hears of the project:
	// the project is made without them, nothing stops for it.
	lib.AnnounceNew(project.ID)
	if err := d.change(func(st *designlyState) { st.Kinds[project.ID] = c.Kind }); err != nil {
		return wire.DesignlyCreated{}, notStarted(err.Error())
	}
	art, file, instructions, err := lib.PrepareDialog(project.ID)
	if err != nil {
		return wire.DesignlyCreated{}, notStarted(err.Error())
	}
	task := lib.NewProjectPrompt(project.Title, brief) + "\n\n" + lib.PagesHint(project.ID) + "\n\n" + kindLine(c.Kind)
	if systemLine != "" {
		task += "\n" + systemLine
	}
	prompt := design.ContinuePrompt(art, task)
	files := []string{file}
	if c.Provider != wire.ProviderClaude {
		// Only Claude takes instruction files; another agent gets the rules in its first message.
		prompt, files = instructions+"\n\n"+prompt, nil
	}
	var model *wire.Model
	if c.Model != nil {
		model = &wire.Model{ID: *c.Model, Effort: c.Effort}
	}
	dir := lib.ArtifactDir(project.ID)
	d.v.starter.promptFiles.Store(dir, files)
	defer d.v.starter.promptFiles.Delete(dir)
	res := d.v.rt.Do(ctx, "designly", wire.ConversationCreateCmd{OpID: "dz-" + randomToken(), Cwd: dir, Provider: c.Provider, Model: model,
		First: &wire.FirstMessage{ClientID: randomToken(), Text: prompt, AttachmentIDs: []string{}}})
	if res.R != wire.ResultAccepted || res.ID == nil {
		return wire.DesignlyCreated{}, notStarted(string(res.Code))
	}
	// The conversation is the project's: it carries its name, not the start of the first message.
	d.v.rt.Do(ctx, "designly", wire.ConversationRenameCmd{OpID: "dz-" + randomToken(), ID: *res.ID, Title: project.Title})
	return wire.DesignlyCreated{ID: project.ID, Title: project.Title, Conv: *res.ID}, nil
}

func randomToken() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func kindLine(k wire.DesignKind) string {
	switch k {
	case wire.DesignSlides:
		return locale.Pick("The project is a slide deck: every board is one 16:9 slide.", "Проект — презентация: каждая доска — один слайд 16:9.")
	case wire.DesignDS:
		return locale.Pick("The project is a design system: the boards show its components, tokens and rules.", "Проект — дизайн-система: доски показывают её компоненты, токены и правила.")
	}
	return locale.Pick("The project is a website: the boards are its pages.", "Проект — сайт: доски — его страницы.")
}

// systemLine is the line of the first message that names the design system to build on.
func (d *v2Designly) systemLine(sys string) (string, bool) {
	for _, s := range d.systems() {
		if s.info.ID != sys {
			continue
		}
		switch {
		case s.dir != "":
			return locale.Pick("Build it on the design system \""+s.info.Label+"\": its folder is "+s.dir+"; read its README first.",
				"Строй на дизайн-системе «"+s.info.Label+"»: её папка "+s.dir+", сначала прочитай её README."), true
		case strings.HasPrefix(s.info.ID, "kit:"):
			return locale.Pick("Build it on the UI kit \""+s.info.Label+"\" installed on this machine ("+s.info.Desc+").",
				"Строй на UI-ките «"+s.info.Label+"», он установлен на машине ("+s.info.Desc+")."), true
		}
		return locale.Pick("Build it on the design system of the project \""+s.info.Label+"\" of the library.",
			"Строй на дизайн-системе проекта «"+s.info.Label+"» из библиотеки."), true
	}
	return "", false
}

func (d *v2Designly) Pin(_ context.Context, id string, pinned bool) error {
	// A project the person has no role in answers as a missing one.
	if d.api.L.ReadArtifact(id) == nil || !d.api.L.Can(id, design.RoleView) {
		return designlyFail(wire.RefuseNotFound, locale.Pick("The design is gone", "Макета больше нет"), "")
	}
	return d.change(func(st *designlyState) {
		if pinned {
			st.Pinned[id] = true
		} else {
			delete(st.Pinned, id)
		}
	})
}

// Line is a design attached to a message of conversation conv (convo.Refs): where the project is and the rules a
// design dialog works by. The conversation is linked to the design, so the library lists it among the design's chats.
func (d *v2Designly) Line(conv string, r wire.MessageRef) (string, wire.RefuseCode) {
	lib := d.api.L
	art, rules, _, err := lib.PrepareDialog(r.ID)
	switch {
	case errors.Is(err, design.ErrGone):
		return "", wire.RefuseNotFound
	case err != nil:
		return "", wire.RefuseInternal
	}
	if err := d.change(func(st *designlyState) { attachDesign(st, conv, art.ID) }); err != nil {
		if log := lib.Config().Log; log != nil {
			log("warn", "design: the chat a design was attached to was not recorded", err)
		}
	}
	return lib.AttachedLine(art, rules), ""
}

// v2Refs gives the conversations the library's designs once it is there (it is made after the runtime).
type v2Refs struct{ v *v2Live }

func (r v2Refs) Line(conv string, ref wire.MessageRef) (string, wire.RefuseCode) {
	if r.v.designly == nil {
		return "", wire.RefuseUnsupported
	}
	return r.v.designly.Line(conv, ref)
}

func (d *v2Designly) Library(ctx context.Context) (wire.DesignLibrary, error) {
	lib := d.api.L
	port, err := lib.EnsureServer()
	if err != nil {
		return wire.DesignLibrary{}, designlyFail(wire.RefuseInternal, locale.Pick("Could not open the design library", "Не удалось открыть библиотеку макетов"), "")
	}
	convs, err := d.v.rt.Conversations(ctx)
	if err != nil {
		return wire.DesignLibrary{}, err
	}
	st := d.state()
	arts := lib.VisibleTo(lib.ListArtifacts())
	out := wire.DesignLibrary{Port: port, Root: lib.Root(), Artifacts: make([]wire.DesignArtifact, 0, len(arts))}
	touched := d.touched(convs.Conversations)
	// Without an identity nobody is "me": the owners still show.
	me, _ := lib.Identity()
	now := time.Now()
	for _, a := range arts {
		dir := lib.ArtifactDir(a.ID)
		item := wire.DesignArtifact{ID: a.ID, Title: a.Title, UpdatedAt: wire.Ms(a.UpdatedAt), Kind: kindOf(st, a.ID), Pinned: st.Pinned[a.ID],
			ProjectPath: a.ProjectPath, Boards: []wire.DesignBoard{}, Convs: []string{}, Role: wire.DesignRole(lib.RoleOf(a.ID, me, now))}
		if a.Cover != nil && *a.Cover != "" {
			item.Cover = a.Cover
		}
		if owner, ok := lib.ReadOwner(a.ID); ok {
			item.Owner = &wire.DesignOwner{Name: owner.Name, Me: owner.Mine(me)}
			lib.ClaimOnce(a.ID, owner, me)
		}
		for _, b := range a.Boards {
			title := b.Title
			if title == "" {
				title = b.Path
			}
			if b.Path != "" && len(item.Boards) < wire.DesignBoardsMax {
				item.Boards = append(item.Boards, wire.DesignBoard{Title: title, Path: b.Path})
			}
		}
		for _, c := range convs.Conversations {
			if c.Cwd == dir || strings.HasPrefix(c.Cwd, dir+string(filepath.Separator)) || attachedTo(st, c.ID, a.ID) {
				item.Convs = append(item.Convs, c.ID)
			}
		}
		for _, id := range touched[a.ID] {
			if !slices.Contains(item.Convs, id) {
				item.TouchedBy = append(item.TouchedBy, id)
			}
		}
		out.Artifacts = append(out.Artifacts, item)
	}
	return out, nil
}

// touched are, by design, the conversations whose transcripts worked on it, the latest activity first.
func (d *v2Designly) touched(convs []wire.Conversation) map[string][]string {
	if d.links == nil {
		return nil
	}
	transcriptOf := map[string]string{}
	last := map[string]int64{}
	paths := make([]string, 0, len(convs))
	for _, c := range convs {
		if c.TranscriptPath == "" {
			continue
		}
		transcriptOf[c.ID] = c.TranscriptPath
		last[c.ID] = int64(c.LastActivityAt)
		paths = append(paths, c.TranscriptPath)
	}
	return design.ChatsOf(d.links.Designs(paths), transcriptOf, last)
}

// backfill reads behind the transcripts of every conversation, for the links of the chats from before; until ctx ends.
func (d *v2Designly) backfill(ctx context.Context) {
	if d.links == nil {
		return
	}
	d.links.Backfill(d.v.rt.Transcripts())
	d.links.Run(ctx)
}

// designSystem is a design system of the machine and where its files are.
type designSystem struct {
	info wire.DesignSystemInfo
	dir  string
}

var uuidSuffix = regexp.MustCompile(`-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// readmeInfo is the heading and the first line of prose of a README, "" when there is none.
func readmeInfo(dir string) (label, desc string) {
	raw, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "<!--"):
		case strings.HasPrefix(line, "#"):
			if label == "" {
				label = strings.TrimSpace(strings.TrimLeft(line, "#"))
			}
		default:
			if desc == "" {
				desc = line
			}
		}
		if label != "" && desc != "" {
			break
		}
	}
	if rs := []rune(desc); len(rs) > 160 {
		desc = string(rs[:159]) + "…"
	}
	return label, desc
}

func prettify(base string) string {
	s := strings.ReplaceAll(base, "-", " ")
	if s == "" {
		return base
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// systems are the design systems the machine has: those the library's template carries in its project/_ds, the projects
// of the library made as design systems, and the UI kits installed on the machine.
func (d *v2Designly) systems() []designSystem {
	lib := d.api.L
	var out []designSystem
	seen := map[string]bool{}
	add := func(s designSystem) {
		if !seen[s.info.ID] {
			seen[s.info.ID] = true
			out = append(out, s)
		}
	}
	scan := func(projectRoot string) {
		dsDir := filepath.Join(projectRoot, design.ProjectDirname, "_ds")
		entries, err := os.ReadDir(dsDir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			base := uuidSuffix.ReplaceAllString(e.Name(), "")
			dir := filepath.Join(dsDir, e.Name())
			label, desc := readmeInfo(dir)
			if label == "" {
				label = prettify(base)
			}
			add(designSystem{info: wire.DesignSystemInfo{ID: base, Label: label, Desc: desc}, dir: dir})
		}
	}
	scan(lib.TemplateDir())
	st := d.state()
	var ds []string
	for id, k := range st.Kinds {
		if k == wire.DesignDS {
			ds = append(ds, id)
		}
	}
	sort.Strings(ds)
	for _, id := range ds {
		a := lib.ReadArtifact(id)
		if a == nil || !lib.Can(id, design.RoleView) {
			continue
		}
		scan(lib.ArtifactDir(id))
		add(designSystem{info: wire.DesignSystemInfo{ID: id, Label: a.Title, Desc: locale.Pick("A design system project of the library", "Проект-дизайн-система из библиотеки")}})
	}
	for _, k := range designkit.List(lib.KitsDir()) {
		if len(k.Versions) == 0 {
			continue
		}
		add(designSystem{info: wire.DesignSystemInfo{ID: "kit:" + k.ID, Label: k.ID, Desc: locale.Pick("UI kit, version ", "UI-кит, версия ") + k.Versions[0].Version}})
	}
	return out
}

func (d *v2Designly) Systems(context.Context) ([]wire.DesignSystemInfo, error) {
	out := []wire.DesignSystemInfo{}
	for _, s := range d.systems() {
		out = append(out, s.info)
	}
	return out, nil
}
