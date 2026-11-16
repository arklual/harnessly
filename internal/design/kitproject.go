package design

import (
	"html"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A project on a kit: the project remembers its kit (meta/project.json "kit", mirrored as "kits": [{id,
// version}]), a new board starts with the kit's files and provider, and an agent of any chat is handed a short block
// about it: which kit, draw only from it, read it with kit_*, check with strict and a screenshot. A project without
// a kit on a machine that has kits asks the person once (kitAsk).

const kitAskKey = "kitAsk"

// KitNone is the answer «no kit» of the one question a project asks.
const KitNone = "none"

// KitAsked is the person's answer to «which kit»: a kit id, KitNone, or "" while it was not asked.
func (l *Library) KitAsked(project string) string {
	if o, ok := readSmallJSON(filepath.Join(l.MetaDir(project), MetaProjectFile)); ok {
		if s, ok := get(o, kitAskKey).(string); ok && (s == KitNone || designkit.ValidID(s)) {
			return s
		}
	}
	return ""
}

// UseProjectKit pins the project to the installed kit named ref (id[@version][/theme]) or records «no kit» for ref
// "none". It returns the pin (nil for «no kit») or the words for the person.
func (l *Library) UseProjectKit(project, ref string) (*KitPin, error) {
	ref = strings.TrimSpace(ref)
	if ref == KitNone {
		return nil, l.setKitAsk(project, KitNone, true)
	}
	pin, msg := l.kitPinFor(ref)
	if pin == nil {
		return nil, &KitPinError{msg}
	}
	if old, ok := l.KitPinOf(project); ok && old.ID == pin.ID && old.Dismissed != "" && pin.Dismissed == "" {
		pin.Dismissed = old.Dismissed
	}
	if err := l.SetKitPin(project, pin); err != nil {
		return nil, err
	}
	return pin, l.setKitAsk(project, pin.ID, false)
}

func (l *Library) setKitAsk(project, answer string, clearPin bool) error {
	m := l.lockOf("kitpin\x00" + project)
	m.Lock()
	defer m.Unlock()
	model := l.ReadModel(project)
	if model == nil {
		return &KitPinError{locale.Pick("The project has no model folder", "У проекта нет папки meta")}
	}
	meta := model.Meta
	if meta.raw == nil {
		meta.raw = jsvalue.NewObject()
	} else {
		meta.raw = meta.raw.Copy()
	}
	meta.raw.Set(kitAskKey, answer)
	if clearPin {
		meta.raw.Delete(kitPinKey)
		meta.Kits = []any{}
	}
	_, err := l.writeProjectMeta(project, meta)
	return err
}

// mirrorKits is the "kits" list of meta/project.json for a pin: [{id, version}].
func mirrorKits(pin *KitPin) []any {
	if pin == nil {
		return []any{}
	}
	return []any{jsvalue.NewObject("id", pin.ID, "version", pin.Version)}
}

// pinMount is the name under ds/ the project's new boards address the kit by.
func (l *Library) pinMount(pin KitPin) string {
	if len(pin.Mounts) > 0 {
		return pin.Mounts[0]
	}
	version := pin.Version
	if pin.Draft {
		version = designkit.DraftVersion
	}
	themes := designkit.Themes(l.KitsDir(), pin.ID, version)
	if len(themes) == 0 {
		return pin.ID
	}
	return kitThemeFolder(themes[0].Folder, pin.ID)
}

var (
	supportScriptRe = regexp.MustCompile(`(?i)<script[^>]*src="\./support\.js"[^>]*></script>\n?`)
	kitBoardMainRe  = regexp.MustCompile(`(?s)(<main class="board">)(.*?)(</main>)`)
)

// kitSeed is the board template of a project on a kit: the kit's tokens, fonts, styles and script are linked, and the
// board's content sits inside the kit's provider, the way every board on the kit starts. A template that already
// addresses the kit (ds/<mount>/) or has no support.js is left as it is.
func (l *Library) kitSeed(project string, seed []byte) []byte {
	pin, ok := l.KitPinOf(project)
	if !ok {
		return seed
	}
	mount := l.pinMount(pin)
	if strings.Contains(string(seed), "ds/"+mount+"/") {
		return seed
	}
	c, err := l.strictCanvas(project)
	if err != nil {
		return seed
	}
	loc := supportScriptRe.FindIndex(seed)
	if loc == nil {
		return seed
	}
	ds := "ds/" + mount + "/"
	exists := func(rel string) bool {
		segs := strings.Split(rel, "/")
		if t := kitFolderOfMount(l, pin, mount); t != "" {
			segs = append(strings.Split(t, "/"), segs...)
		}
		return designkit.Resolve(l.KitsDir(), c.Kit, c.Version, segs) != ""
	}
	var head strings.Builder
	for _, css := range []string{"tokens.css", "fonts.css", "components/bundle.css"} {
		if exists(css) {
			head.WriteString("<link rel=\"stylesheet\" href=\"" + html.EscapeString(ds+css) + "\">\n")
		}
	}
	if exists("components/bundle.js") {
		head.WriteString("<script src=\"" + html.EscapeString(ds+"components/bundle.js") + "\"></script>\n")
	}
	out := append(append(append([]byte{}, seed[:loc[1]]...), head.String()...), seed[loc[1]:]...)
	if c.Root != "" && compNameRe.MatchString(c.Root) && compNameRe.MatchString(firstNonEmpty(c.RootNs, c.Namespace)) {
		root := firstNonEmpty(c.RootNs, c.Namespace) + "." + c.Root
		out = kitBoardMainRe.ReplaceAll(out, []byte("${1}\n  <x-import component-from-global-scope=\""+root+"\">\n${2}  </x-import>\n${3}"))
	}
	return out
}

// kitFolderOfMount is the folder (under the kit's version) of the theme mounted as ds/<mount>/; "" for a kit that
// mounts whole.
func kitFolderOfMount(l *Library, pin KitPin, mount string) string {
	version := pin.Version
	if pin.Draft {
		version = designkit.DraftVersion
	}
	for _, t := range designkit.Themes(l.KitsDir(), pin.ID, version) {
		if t.Folder != "" && path.Base(t.Folder) == mount {
			return t.Folder
		}
	}
	return ""
}

// InstalledKitIDs are the kits installed on this machine, newest version first in the second list.
func (l *Library) installedKits() []designkit.Kit {
	kits := designkit.List(l.KitsDir())
	sort.Slice(kits, func(i, j int) bool { return kits[i].ID < kits[j].ID })
	return kits
}

// KitBrief is the short block an agent is handed about the project's kit: «draw only from it», how to read it and how
// to check the result. "" for a project without a kit. When the project has none but the machine has kits and the
// person was not asked yet, it is the one question to ask. Project is the library id.
func (l *Library) KitBrief(project string) string {
	cli := l.cfg.Texts.CLI
	if cli == "" {
		cli = "harnessly"
	}
	pin, ok := l.KitPinOf(project)
	if !ok {
		return l.kitAskBrief(project, cli)
	}
	where := pin.ID + " " + pin.Version
	if pin.Draft {
		where = pin.ID + " (draft)"
	}
	mount := l.pinMount(pin)
	ns, root := "", ""
	if c, err := l.strictCanvas(project); err == nil {
		ns, root = c.Namespace, c.Root
	}
	if !locale.Russian() {
		return "UI kit of this project: " + where + " (ds/" + mount + "/, " + ns + ".*" + rootSuffix(root) + "). Draw ONLY from it: no own HTML, colours or sizes.\n" +
			"- Read it first: kit_index {kit:\"" + pin.ID + "\"}, then kit_component for every component you use (when to use / not, props, sampleJsx), kit_tokens for variables, kit_templates for a known kind of page.\n" +
			"- Boards are <x-import component-from-global-scope=\"" + ns + ".Name\" …>" + providerNote(root) + ".\n" +
			"- After an edit: `" + cli + " design check " + project + " --strict` must say ok; then `" + cli + " design check " + project + " --board FILE` takes a screenshot (.check/): open the screenshot and look at it, fix what is off. Strict mode is on for this project.\n" +
			"- The work is NOT done until `" + cli + " design check " + project + " --strict` says ok: fix every finding and run it again. Layout, text and icons come from the kit too (Stack, Grid, Text, Glyph, DataTable, TextField, SelectField where the kit has them: kit_index): no <div>, <table>, <input>, no px or colours of your own."
	}
	return "UI-кит проекта: " + where + " (ds/" + mount + "/, " + ns + ".*" + rootSuffix(root) + "). Рисуй ТОЛЬКО из него: свой HTML, цвета и размеры не придумывай.\n" +
		"- Сначала прочитай кит: kit_index {kit:\"" + pin.ID + "\"}, затем kit_component на каждый нужный компонент (когда и когда не, props, sampleJsx), kit_tokens — переменные, kit_templates — страница известного вида.\n" +
		"- Доска — <x-import component-from-global-scope=\"" + ns + ".Имя\" …>" + providerNoteRu(root) + ".\n" +
		"- После правки: `" + cli + " design check " + project + " --strict` должен сказать ok; затем `" + cli + " design check " + project + " --board ФАЙЛ` снимает скриншот (.check/): открой скриншот и посмотри глазами, поправь, что не так. Строгий режим у проекта включён.\n" +
		"- Работа не готова, пока `" + cli + " design check " + project + " --strict` не даёт ok: поправь каждое замечание и запусти снова. Раскладка, текст и иконки тоже из кита (Stack, Grid, Text, Glyph, DataTable, TextField, SelectField, если они есть в ките: kit_index): ни <div>, <table>, <input>, ни своих px и цветов."
}

// providerNote says where the kit's root sits: only a kit that has one wraps new boards in it.
func providerNote(root string) string {
	if root == "" {
		return ""
	}
	return " inside the provider that new boards already have"
}

func providerNoteRu(root string) string {
	if root == "" {
		return ""
	}
	return " внутри провайдера, который в новой доске уже есть"
}

func rootSuffix(root string) string {
	if root == "" {
		return ""
	}
	return ", root " + root
}

// kitAskBrief is the question for a project with no kit while the machine has kits: asked once, the answer is kept.
func (l *Library) kitAskBrief(project, cli string) string {
	if l.KitAsked(project) != "" {
		return ""
	}
	kits := l.installedKits()
	if len(kits) == 0 {
		return ""
	}
	ids := make([]string, len(kits))
	for i, k := range kits {
		ids[i] = k.ID
	}
	list := strings.Join(ids, ", ")
	if !locale.Russian() {
		return "This project has no UI kit, and the machine has: " + list + ". If the task is a NEW design and the person did not name a kit, ask once with notice_ask (a question with options, the first is the one that fits the task, marked recommended): «There is the kit X, it seems to fit the task. Use it or another?» Options: X (recommended), another installed kit, no kit. Then run `" + cli + " design kit use " + project + " <kit>` (or `none`): the answer is kept, do not ask again. A kit the person named in the request: run `kit use` at once, ask nothing."
	}
	return "У проекта нет UI-кита, а на машине есть: " + list + ". Если задача — НОВЫЙ дизайн и человек не назвал кит, один раз спроси карточкой notice_ask (вопрос с вариантами, первый — подходящий к задаче, с пометкой «рекомендуется»): «Есть кит X, кажется, он подходит к задаче. Использовать его или другой?» Варианты: X (рекомендуется), другой из установленных, без кита. Потом выполни `" + cli + " design kit use " + project + " <кит>` (или `none`): ответ запомнится, второй раз не спрашивай. Кит назван в просьбе — сразу `kit use`, ничего не спрашивай."
}

// BriefOnce is KitBrief the first time a chat session asks for a project, "" after that.
func (l *Library) BriefOnce(session, project string) string {
	if session == "" {
		return l.KitBrief(project)
	}
	b := l.KitBrief(project)
	if b == "" {
		return ""
	}
	key := session + "\x00" + project + "\x00" + b
	if _, dup := l.briefed.LoadOrStore(key, true); dup {
		return ""
	}
	return b
}
