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

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
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
	return &CreateError{Msg: "Проект «" + id + "» уже есть", Conflict: true}
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
		return "", createErr("Назовите проект")
	}
	if jscompat.Len(t) > LimitTitle {
		return "", createErr("Название длиннее " + strconv.Itoa(LimitTitle) + " знаков")
	}
	return t, nil
}

func (l *Library) checkTemplate() error {
	dir := l.TemplateDir()
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return createErr("В библиотеке нет шаблона нового проекта: положите его в " + dir)
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
		return createErr("Шаблон нового проекта " + dir + " ведёт за пределы библиотеки")
	}
	if st, err := os.Lstat(filepath.Join(dir, ProjectDirname)); err != nil || !st.IsDir() {
		return createErr("В шаблоне нового проекта " + dir + " нет папки " + ProjectDirname + "/")
	}
	return nil
}

func (l *Library) readTemplateMeta() (*jsvalue.Object, error) {
	file := filepath.Join(l.TemplateDir(), ArtifactFile)
	broken := createErr("Шаблон нового проекта повреждён: не читается " + TemplateDirname + "/" + ArtifactFile)
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
		return nil, createErr("Шаблон нового проекта повреждён: " + TemplateDirname + "/" + ArtifactFile + " не объект")
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
		return createErr("Шаблон нового проекта больше, чем можно скопировать")
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
		return createErr("Шаблон нового проекта ведёт за пределы библиотеки")
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
			return createErr("В шаблоне нового проекта слишком много файлов")
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

func (l *Library) copyInto(id, title string, meta *jsvalue.Object) (CreatedProject, error) {
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
	realTemplate, err := filepath.EvalSymlinks(l.TemplateDir())
	if err != nil {
		return CreatedProject{}, err
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
			return createErr("Папка нового проекта подменена во время создания")
		}
		if err := copyTemplate(l.TemplateDir(), into, realTemplate, &copyBudget{bytes: LimitUnpackedBytes, entries: LimitEntries}); err != nil {
			return err
		}
		m := meta.Copy()
		m.Set("v", 1)
		m.Set("title", title)
		m.Set("importedAt", isoNow())
		m.Set("boards", []any{})
		return writeCreatedMeta(into, m)
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
		return CreatedProject{}, createErr("Папка нового проекта подменена во время создания")
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
	if err := l.checkTemplate(); err != nil {
		return CreatedProject{}, err
	}
	meta, err := l.readTemplateMeta()
	if err != nil {
		return CreatedProject{}, err
	}
	if err := os.MkdirAll(l.Root(), 0o777); err != nil {
		return CreatedProject{}, err
	}
	for attempt := 1; ; attempt++ {
		p, err := l.copyInto(SlugifyArtifactID(shown, l.TakenArtifactIDs()), shown, meta)
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
		return "", createErr("Опишите, что нарисовать")
	}
	if jscompat.Len(b) > 4000 {
		return "", createErr("Описание длиннее 4000 знаков")
	}
	return b, nil
}

// NewProjectPrompt is the first message of the chat a new project opens.
func (l *Library) NewProjectPrompt(title, brief string) string {
	t := l.cfg.Texts
	return strings.Join([]string{
		"Новый проект «" + jscompat.Trim(spaceRun.ReplaceAllString(title, " ")) + "» создан из шаблона библиотеки. Задача от человека:",
		"",
		brief,
		"",
		"Доски делай рядом с `project/_board-template.html`: копия шаблона под нужным именем с расширением `.dc.html`,",
		"разметка внутри `<main class=\"board\">`. Собирай их из компонентов UI-кита (`window." + t.KitGlobal + "`, README в `project/_ds/*/`);",
		"как это выглядит в живых досках — смотри соседние проекты библиотеки. Каждая новая `*.dc.html` в `project/` появляется",
		"в библиотеке сама. Когда доски готовы, проверь их: `" + t.CLI + " design check <id> --board \"<файл доски>\"`.",
	}, "\n")
}
