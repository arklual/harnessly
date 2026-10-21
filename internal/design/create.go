package design

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A new project of the library: the library's .template/ folder copied to <library>/<id>/ with the title and the
// creation time in artifact.json, built aside and renamed into place. The template is a folder anyone with access can
// write to, so it gets the importer's distrust, and both sides of the copy go through descriptors opened without
// following links.

const TemplateDirname = ".template"

// CreateError is a refusal worded for the person; Conflict when the id is taken.
type CreateError struct {
	Msg      string
	Conflict bool
}

func (e *CreateError) Error() string { return e.Msg }

func createErr(msg string) error { return &CreateError{Msg: msg} }

func createConflict(id string) error {
	return &CreateError{Msg: locale.Pick("Project \""+id+"\" already exists", "Проект «"+id+"» уже есть"), Conflict: true}
}

var (
	titleControls = regexp.MustCompile("[\x00-\x1f\x7f-\u009f  ]+")
	briefControls = regexp.MustCompile("[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]+")
	mdFile        = regexp.MustCompile(`(?i)\.mdx?$`)
	agentWord     = regexp.MustCompile(`(?i)claude|agents?|gemini|copilot`)
)

func (l *Library) TemplateDir() string { return filepath.Join(l.Root(), TemplateDirname) }

func cleanProjectTitle(v string) (string, error) {
	t := jscompat.Trim(spaceRun.ReplaceAllString(titleControls.ReplaceAllString(v, " "), " "))
	if t == "" {
		return "", createErr(locale.Pick("Name the project", "Назовите проект"))
	}
	if jscompat.Len(t) > LimitTitle {
		return "", createErr(locale.Pick("The title is longer than "+strconv.Itoa(LimitTitle)+" characters", "Название длиннее "+strconv.Itoa(LimitTitle)+" знаков"))
	}
	return t, nil
}

func (l *Library) checkTemplate() error {
	dir := l.TemplateDir()
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return createErr(locale.Pick("The library has no new-project template: put it in "+dir, "В библиотеке нет шаблона нового проекта: положите его в "+dir))
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(l.Root())
	if err != nil {
		return err
	}
	if !isInside(real, root) {
		return createErr(locale.Pick("The new-project template "+dir+" leads outside the library", "Шаблон нового проекта "+dir+" ведёт за пределы библиотеки"))
	}
	if st, err := os.Lstat(filepath.Join(dir, ProjectDirname)); err != nil || !st.IsDir() {
		return createErr(locale.Pick("The new-project template "+dir+" has no "+ProjectDirname+"/ folder", "В шаблоне нового проекта "+dir+" нет папки "+ProjectDirname+"/"))
	}
	return nil
}

func (l *Library) readTemplateMeta() (*jsvalue.Object, error) {
	file := filepath.Join(l.TemplateDir(), ArtifactFile)
	broken := createErr(locale.Pick("The new-project template is damaged: "+TemplateDirname+"/"+ArtifactFile+" cannot be read", "Шаблон нового проекта повреждён: не читается "+TemplateDirname+"/"+ArtifactFile))
	if st, err := os.Lstat(file); err != nil || !st.Mode().IsRegular() {
		return nil, broken
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, broken
	}
	raw, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(b)))
	if err != nil {
		return nil, broken
	}
	o, ok := raw.(*jsvalue.Object)
	if !ok {
		return nil, createErr(locale.Pick("The new-project template is damaged: "+TemplateDirname+"/"+ArtifactFile+" is not an object", "Шаблон нового проекта повреждён: "+TemplateDirname+"/"+ArtifactFile+" не объект"))
	}
	return o, nil
}

// namesAnAgent is wider than the importer's list: any markdown naming an agent stays out of a template's copy.
func namesAnAgent(name string) bool { return mdFile.MatchString(name) && agentWord.MatchString(name) }

type copyBudget struct{ bytes, entries int64 }

func copyFileNoFollow(source, target string, budget *copyBudget) error {
	from, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer from.Close()
	st, err := from.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return nil
	}
	budget.bytes -= st.Size()
	if budget.bytes < 0 {
		return createErr(locale.Pick("The new-project template is larger than can be copied", "Шаблон нового проекта больше, чем можно скопировать"))
	}
	to, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, st.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(to, io.LimitReader(from, st.Size()))
	if cerr := to.Close(); err == nil {
		err = cerr
	}
	return err
}

// copyTemplate copies regular files and folders only, and none of what an agent would take for instructions or
// settings; modes are the template's permission bits without setgid or sticky.
func copyTemplate(from string, into *os.File, realRoot string, budget *copyBudget) error {
	dir, err := openDirNoFollow(from)
	if err != nil {
		return err
	}
	defer dir.Close()
	link, err := os.Readlink(viaFd(dir, ""))
	if err != nil {
		return err
	}
	if !isInside(link, realRoot) {
		return createErr(locale.Pick("The new-project template leads outside the library", "Шаблон нового проекта ведёт за пределы библиотеки"))
	}
	entries, err := os.ReadDir(viaFd(dir, ""))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if IsAgentEntry(e) || namesAnAgent(e.Name()) {
			continue
		}
		budget.entries--
		if budget.entries < 0 {
			return createErr(locale.Pick("The new-project template has too many files", "В шаблоне нового проекта слишком много файлов"))
		}
		switch {
		case e.IsDir():
			if err := os.Mkdir(viaFd(into, e.Name()), 0o777); err != nil {
				return err
			}
			child, err := openDirNoFollow(viaFd(into, e.Name()))
			if err != nil {
				return err
			}
			err = copyTemplate(viaFd(dir, e.Name()), child, realRoot, budget)
			child.Close()
			if err != nil {
				return err
			}
		case e.Type().IsRegular():
			if err := copyFileNoFollow(viaFd(dir, e.Name()), viaFd(into, e.Name()), budget); err != nil {
				return err
			}
		}
	}
	st, err := dir.Stat()
	if err != nil {
		return err
	}
	return into.Chmod(st.Mode().Perm())
}

// writeCreatedMeta writes artifact.json of the copy to a fresh file renamed over the template's copy, so whatever sits
// at that name is replaced, never written through.
func writeCreatedMeta(into *os.File, meta *jsvalue.Object) error {
	temp := "." + ArtifactFile + "." + randomHex(8) + ".tmp"
	f, err := os.OpenFile(viaFd(into, temp), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o664)
	if err != nil {
		return err
	}
	_, err = f.Write(append(jsvalue.Stringify(meta, "  "), '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(viaFd(into, temp), viaFd(into, ArtifactFile))
}

func isNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

// CreatedProject is a new project of the library.
type CreatedProject struct{ ID, Title, Dir string }

func (l *Library) copyInto(id, title string, meta *jsvalue.Object, own bool) (CreatedProject, error) {
	target := l.ArtifactDir(id)
	if exists(target) {
		return CreatedProject{}, createConflict(id)
	}
	stageName := ".creating-" + id + "-" + randomHex(8)
	stage := filepath.Join(l.Root(), stageName)
	defer os.RemoveAll(stage)
	realLibrary, err := filepath.EvalSymlinks(l.Root())
	if err != nil {
		return CreatedProject{}, err
	}
	realTemplate := ""
	if own {
		if realTemplate, err = filepath.EvalSymlinks(l.TemplateDir()); err != nil {
			return CreatedProject{}, err
		}
	}
	if err := os.Mkdir(stage, 0o777); err != nil {
		return CreatedProject{}, err
	}
	into, err := openDirNoFollow(stage)
	if err != nil {
		return CreatedProject{}, err
	}
	err = func() error {
		defer into.Close()
		if link, err := os.Readlink(viaFd(into, "")); err != nil || link != filepath.Join(realLibrary, stageName) {
			return createErr(locale.Pick("The new project's folder was replaced while it was being created", "Папка нового проекта подменена во время создания"))
		}
		if own {
			if err := copyTemplate(l.TemplateDir(), into, realTemplate, &copyBudget{bytes: LimitUnpackedBytes, entries: LimitEntries}); err != nil {
				return err
			}
		} else if err := l.writeShippedTemplate(into); err != nil {
			return err
		}
		created := isoNow()
		m := meta.Copy()
		m.Set("v", 1)
		m.Set("title", title)
		m.Set("importedAt", created)
		m.Set("boards", []any{})
		if err := writeCreatedMeta(into, m); err != nil {
			return err
		}
		return writeNewMeta(viaFd(into, ""), title, created, l.canvasKey(), l.NewProjectsRelease())
	}()
	if err != nil {
		return CreatedProject{}, err
	}
	if err := os.Rename(stage, target); err != nil {
		if isNotEmpty(err) {
			return CreatedProject{}, createConflict(id)
		}
		return CreatedProject{}, err
	}
	if st, err := os.Lstat(target); err != nil || !st.IsDir() {
		return CreatedProject{}, createErr(locale.Pick("The new project's folder was replaced while it was being created", "Папка нового проекта подменена во время создания"))
	}
	if _, err := l.WriteDerived(id); err != nil {
		l.warn("design: the files built from meta/ of a new project were not written", err)
	}
	return CreatedProject{ID: id, Title: title, Dir: target}, nil
}

// CreateProject makes a new project from the template under an id made from the title (the next free one).
func (l *Library) CreateProject(title string) (CreatedProject, error) {
	defer l.listing.invalidateAll()
	shown, err := cleanProjectTitle(title)
	if err != nil {
		return CreatedProject{}, err
	}
	// Without a template of its own the library gets the one shipped with the runtime.
	own := l.hasOwnTemplate()
	meta := jsvalue.NewObject()
	if own {
		if err := l.checkTemplate(); err != nil {
			return CreatedProject{}, err
		}
		if meta, err = l.readTemplateMeta(); err != nil {
			return CreatedProject{}, err
		}
	}
	if err := os.MkdirAll(l.Root(), 0o777); err != nil {
		return CreatedProject{}, err
	}
	for attempt := 1; ; attempt++ {
		p, err := l.copyInto(SlugifyArtifactID(shown, l.TakenArtifactIDs()), shown, meta, own)
		var ce *CreateError
		if err == nil || !errors.As(err, &ce) || !ce.Conflict || attempt >= 3 {
			return p, err
		}
	}
}

// CleanBrief is the person's brief for a new project: controls gone, trimmed, at most 4000 characters.
func CleanBrief(v any) (string, error) {
	s, _ := v.(string)
	b := jscompat.Trim(briefControls.ReplaceAllString(s, " "))
	if b == "" {
		return "", createErr(locale.Pick("Describe what to draw", "Опишите, что нарисовать"))
	}
	if jscompat.Len(b) > 4000 {
		return "", createErr(locale.Pick("The description is longer than 4000 characters", "Описание длиннее 4000 знаков"))
	}
	return b, nil
}

// NewProjectPrompt is the first message of the chat a new project opens.
func (l *Library) NewProjectPrompt(title, brief string) string {
	t := l.cfg.Texts
	clean := jscompat.Trim(spaceRun.ReplaceAllString(title, " "))
	if !locale.Russian() {
		return strings.Join([]string{
			"The new project \"" + clean + "\" was created from the library template. The person's task:",
			"",
			brief,
			"",
			"Make the boards next to `project/_board-template.html`: a copy of the template under the name you need with the `.dc.html` extension,",
			"markup inside `<main class=\"board\">`. Build them from the UI kit's components (`window." + t.KitGlobal + "`, README in `project/_ds/*/`);",
			"for how that looks in live boards, see the neighbouring projects of the library. Every new `*.dc.html` in `project/` appears",
			"in the library by itself. When the boards are ready, check them: `" + t.CLI + " design check <id> --board \"<board file>\"`.",
		}, "\n")
	}
	return strings.Join([]string{
		"Новый проект «" + clean + "» создан из шаблона библиотеки. Задача от человека:",
		"",
		brief,
		"",
		"Доски делай рядом с `project/_board-template.html`: копия шаблона под нужным именем с расширением `.dc.html`,",
		"разметка внутри `<main class=\"board\">`. Собирай их из компонентов UI-кита (`window." + t.KitGlobal + "`, README в `project/_ds/*/`);",
		"как это выглядит в живых досках — смотри соседние проекты библиотеки. Каждая новая `*.dc.html` в `project/` появляется",
		"в библиотеке сама. Когда доски готовы, проверь их: `" + t.CLI + " design check <id> --board \"<файл доски>\"`.",
	}, "\n")
}

// PagesHint tells the agent of a new project how its pages are made: the project keeps them in meta/.
func (l *Library) PagesHint(id string) string {
	cli := l.cfg.Texts.CLI
	return locale.Pick(
		"The project keeps its pages in meta/: make each board with `"+cli+" design page create "+id+" --title \"<Title>\"`, which "+
			"makes the board file from the template, gives the page a permanent id and prints the file to edit. "+
			"project/canvas.json is built from meta/: do not edit it.",
		"Проект хранит страницы в meta/: каждую доску делай командой `"+cli+" design page create "+id+" --title \"<Название>\"` — "+
			"она сделает файл доски из шаблона, даст странице постоянный номер и напечатает файл, который править. "+
			"project/canvas.json собирается из meta/: его не правь.")
}
